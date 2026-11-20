package scm

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ReviewFiles is a bounded inventory, never file contents or execution evidence.
type ReviewFiles struct {
	Available bool         `json:"available"`
	Complete  bool         `json:"complete"`
	Reason    string       `json:"reason,omitempty"`
	Items     []ReviewFile `json:"items"`
}
type ReviewFile struct {
	Path         string `json:"path"`
	PreviousPath string `json:"previousPath,omitempty"`
	Status       string `json:"status"`
}

// Reported checks do not establish required branch rules or permission to merge.
// Empty reports and unknown provider states must never become a passing verdict.
type ReviewChecks struct {
	Available     bool          `json:"available"`
	Complete      bool          `json:"complete"`
	RequiredRules string        `json:"requiredRules"`
	Reason        string        `json:"reason,omitempty"`
	Items         []ReviewCheck `json:"items"`
}
type ReviewCheck struct {
	Name          string `json:"name"`
	State         string `json:"state"`
	ProviderState string `json:"providerState"`
}
type fileReader interface {
	PullRequestFiles(context.Context, ReviewSnapshot) (ReviewFiles, error)
}
type checkReader interface {
	PullRequestChecks(context.Context, ReviewSnapshot) (ReviewChecks, error)
}

func fileEntry(path, previous, status string) (ReviewFile, error) {
	if path == "" || len(path) > 4096 || len(previous) > 4096 || strings.ContainsAny(path+previous, "\x00\r\n") || status == "" {
		return ReviewFile{}, errors.New("SCM returned an incomplete file inventory")
	}
	return ReviewFile{path, previous, status}, nil
}
func checkEntry(name, state string) (ReviewCheck, error) {
	if name == "" || len(name) > 4096 || len(state) > 256 {
		return ReviewCheck{}, errors.New("SCM returned an incomplete check report")
	}
	normalized := "unknown"
	switch strings.ToUpper(state) {
	case "SUCCESS", "SUCCESSFUL":
		normalized = "passed"
	case "FAILURE", "FAILED", "ERROR", "CANCELLED", "CANCELED", "TIMED_OUT", "ACTION_REQUIRED", "STOPPED":
		normalized = "failed"
	case "PENDING", "INPROGRESS", "IN_PROGRESS", "QUEUED", "WAITING", "REQUESTED":
		normalized = "pending"
	}
	return ReviewCheck{name, normalized, state}, nil
}
func (g *github) PullRequestFiles(ctx context.Context, s ReviewSnapshot) (ReviewFiles, error) {
	out := ReviewFiles{Available: true, Items: []ReviewFile{}}
	h, err := g.reviewHTTP()
	if err != nil {
		return out, err
	}
	var result struct {
		Base struct {
			SHA string `json:"sha"`
		} `json:"base_commit"`
		Files []struct {
			Filename string `json:"filename"`
			Previous string `json:"previous_filename"`
			Status   string `json:"status"`
		} `json:"files"`
	}
	// Compare immutable commit IDs. PR /files pagination names a mutable PR and
	// cannot pin every page to this revision pair. Compare caps files at 300.
	err = h.do(ctx, "GET", fmt.Sprintf("/repos/%s/compare/%s...%s?per_page=1&page=1", g.c.Repository, s.BaseSHA, s.HeadSHA), nil, &result)
	if err != nil {
		return out, err
	}
	if result.Base.SHA != s.BaseSHA || result.Files == nil {
		return out, errors.New("SCM returned an incomplete revision-pinned file inventory")
	}
	if len(result.Files) >= 300 {
		return out, errors.New("SCM file inventory reached the comparison limit; completeness cannot be established")
	}
	for _, r := range result.Files {
		f, e := fileEntry(r.Filename, r.Previous, r.Status)
		if e != nil {
			return out, e
		}
		out.Items = append(out.Items, f)
	}
	out.Complete = true
	return out, nil
}
func (b *bitbucketCloud) PullRequestFiles(ctx context.Context, s ReviewSnapshot) (ReviewFiles, error) {
	out := ReviewFiles{Available: true, Items: []ReviewFile{}}
	path := fmt.Sprintf("%s/diffstat/%s..%s?topic=true&pagelen=100", b.path(), s.HeadSHA, s.BaseSHA)
	for page := 0; page < adapterPageLimit; page++ {
		if err := reviewPageQuery(path, true); err != nil {
			return out, err
		}
		var rows struct {
			Next   string `json:"next"`
			Values []struct {
				Status string `json:"status"`
				Old    *struct {
					Path string `json:"path"`
				} `json:"old"`
				New *struct {
					Path string `json:"path"`
				} `json:"new"`
			} `json:"values"`
		}
		if err := b.http.do(ctx, "GET", path, nil, &rows); err != nil {
			return out, err
		}
		if rows.Values == nil {
			return out, errors.New("SCM returned an incomplete file inventory")
		}
		for _, r := range rows.Values {
			var current, old string
			if r.Old != nil {
				old = r.Old.Path
			}
			if r.New != nil {
				current = r.New.Path
			}
			if current == "" {
				current = old
			}
			if current == old {
				old = ""
			}
			f, e := fileEntry(current, old, r.Status)
			if e != nil {
				return out, e
			}
			out.Items = append(out.Items, f)
		}
		if rows.Next == "" {
			out.Complete = true
			return out, nil
		}
		var err error
		path, err = b.http.nextPage(path, rows.Next)
		if err != nil {
			return out, err
		}
	}
	return out, errors.New("SCM file inventory exceeded the page limit")
}
func (b *bitbucketDC) PullRequestFiles(context.Context, ReviewSnapshot) (ReviewFiles, error) {
	return ReviewFiles{Items: []ReviewFile{}, Reason: "A complete revision-pinned file inventory is not qualified for this adapter. The bounded structured diff is available separately."}, nil
}
func (g *github) PullRequestChecks(ctx context.Context, s ReviewSnapshot) (ReviewChecks, error) {
	out := ReviewChecks{Available: true, RequiredRules: "unavailable", Items: []ReviewCheck{}}
	h, err := g.reviewHTTP()
	if err != nil {
		return out, err
	}
	// Combined statuses contain the latest state per context. Check runs are a
	// separate API and both must be read; neither describes required branch rules.
	for page := 1; page <= adapterPageLimit; page++ {
		var rows struct {
			SHA      string                            `json:"sha"`
			Total    *int                              `json:"total_count"`
			Statuses []struct{ Context, State string } `json:"statuses"`
		}
		if err = h.do(ctx, "GET", fmt.Sprintf("/repos/%s/commits/%s/status?per_page=100&page=%d", g.c.Repository, s.HeadSHA, page), nil, &rows); err != nil {
			return out, err
		}
		if rows.SHA != s.HeadSHA || rows.Total == nil || rows.Statuses == nil {
			return out, errors.New("SCM returned incomplete commit statuses")
		}
		for _, r := range rows.Statuses {
			c, e := checkEntry(r.Context, r.State)
			if e != nil {
				return out, e
			}
			out.Items = append(out.Items, c)
		}
		if len(rows.Statuses) < 100 {
			if len(out.Items) != *rows.Total {
				return out, errors.New("SCM commit status count changed while reading")
			}
			break
		}
		if page == adapterPageLimit {
			return out, errors.New("SCM checks exceeded the page limit")
		}
	}
	count := 0
	for page := 1; page <= adapterPageLimit; page++ {
		var rows struct {
			Total *int `json:"total_count"`
			Runs  []struct {
				Name, Status string
				Conclusion   *string
				HeadSHA      string `json:"head_sha"`
			} `json:"check_runs"`
		}
		if err = h.do(ctx, "GET", fmt.Sprintf("/repos/%s/commits/%s/check-runs?filter=latest&per_page=100&page=%d", g.c.Repository, s.HeadSHA, page), nil, &rows); err != nil {
			return out, err
		}
		if rows.Total == nil || rows.Runs == nil {
			return out, errors.New("SCM returned incomplete check runs")
		}
		for _, r := range rows.Runs {
			if r.HeadSHA != s.HeadSHA {
				return out, errors.New("check belongs to a different commit")
			}
			state := r.Status
			if state == "completed" {
				state = "unknown"
				if r.Conclusion != nil {
					state = *r.Conclusion
				}
			}
			c, e := checkEntry(r.Name, state)
			if e != nil {
				return out, e
			}
			out.Items = append(out.Items, c)
			count++
		}
		if len(rows.Runs) < 100 {
			if count != *rows.Total {
				return out, errors.New("SCM check count changed while reading")
			}
			out.Complete = true
			return out, nil
		}
	}
	return out, errors.New("SCM checks exceeded the page limit")
}
func (b *bitbucketCloud) PullRequestChecks(ctx context.Context, s ReviewSnapshot) (ReviewChecks, error) {
	out := ReviewChecks{Available: true, RequiredRules: "unavailable", Items: []ReviewCheck{}}
	path := fmt.Sprintf("%s/commit/%s/statuses/build?pagelen=100", b.path(), s.HeadSHA)
	for page := 0; page < adapterPageLimit; page++ {
		if err := reviewPageQuery(path, false); err != nil {
			return out, err
		}
		var rows struct {
			Next   string                        `json:"next"`
			Values []struct{ Key, State string } `json:"values"`
		}
		if err := b.http.do(ctx, "GET", path, nil, &rows); err != nil {
			return out, err
		}
		if rows.Values == nil {
			return out, errors.New("SCM returned incomplete build statuses")
		}
		for _, r := range rows.Values {
			c, e := checkEntry(r.Key, r.State)
			if e != nil {
				return out, e
			}
			out.Items = append(out.Items, c)
		}
		if rows.Next == "" {
			out.Complete = true
			return out, nil
		}
		var err error
		path, err = b.http.nextPage(path, rows.Next)
		if err != nil {
			return out, err
		}
	}
	return out, errors.New("SCM checks exceeded the page limit")
}
func (b *bitbucketDC) PullRequestChecks(ctx context.Context, s ReviewSnapshot) (ReviewChecks, error) {
	out := ReviewChecks{Available: true, RequiredRules: "unavailable", Items: []ReviewCheck{}}
	start := 0
	for page := 0; page < adapterPageLimit; page++ {
		var rows dcPage[struct{ Key, State string }]
		// The dedicated build-status API uses the configured site/context, never
		// the repository API base or a provider-supplied cross-origin URL.
		path := fmt.Sprintf("/rest/build-status/1.0/commits/%s?limit=100&start=%d", s.HeadSHA, start)
		if err := b.http.do(ctx, "GET", path, nil, &rows); err != nil {
			return out, err
		}
		next, err := rows.next(start)
		if err != nil {
			return out, err
		}
		for _, r := range rows.Values {
			c, e := checkEntry(r.Key, r.State)
			if e != nil {
				return out, e
			}
			out.Items = append(out.Items, c)
		}
		if next < 0 {
			out.Complete = false
			out.Reason = "This provider API returns at most the 100 most recent builds. Older reported checks and required rules are not established."
			return out, nil
		}
		start = next
	}
	return out, errors.New("SCM checks exceeded the page limit")
}

// next links may advance pagination, never change the reviewed resource's
// comparison mode or introduce provider-side filters that hide evidence.
func reviewPageQuery(path string, topic bool) error {
	u, err := url.Parse(path)
	if err != nil {
		return errors.New("invalid SCM evidence pagination")
	}
	q, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return errors.New("invalid SCM evidence pagination")
	}
	for key, values := range q {
		if len(values) != 1 {
			return errors.New("ambiguous SCM evidence pagination")
		}
		switch key {
		case "topic":
			if !topic || values[0] != "true" {
				return errors.New("SCM pagination changed the comparison mode")
			}
		case "page":
			if values[0] == "" || len(values[0]) > 512 {
				return errors.New("invalid SCM evidence page")
			}
		case "pagelen":
			n, e := strconv.Atoi(values[0])
			if e != nil || n < 1 || n > 100 {
				return errors.New("invalid SCM evidence page size")
			}
		default:
			return errors.New("SCM pagination introduced an unreviewed evidence filter")
		}
	}
	if topic && q.Get("topic") != "true" {
		return errors.New("SCM pagination changed the comparison mode")
	}
	return nil
}
