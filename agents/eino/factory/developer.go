package factory

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"regexp"
	"strings"

	"github.com/woliveiras/weft/agents/eino/protocol"
)

type fileChange struct {
	Path    string  `json:"path"`
	Content *string `json:"content"`
}
type change struct {
	Files         []fileChange
	Summary       string
	CommitMessage string `json:"commit_message"`
}

func Developer(ctx context.Context) (protocol.Agent, error) {
	return messageAgent(ctx, "develop", map[string]action{"task.ready": develop, "qa.failed": develop, "review.changes_requested": develop})
}

var invalidSlug = regexp.MustCompile(`[^a-z0-9]+`)

func scaffold(id, title string) change {
	slug := strings.Trim(invalidSlug.ReplaceAllString(strings.ToLower(title), "_"), "_")
	if len(slug) > 30 {
		slug = slug[:30]
	}
	hash := sha256.Sum256([]byte(id))
	dir := fmt.Sprintf("src/%s_%x", first(slug, "item"), hash[:3])
	source := fmt.Sprintf("package feature\n\nfunc TaskID() string { return %q }\n", id)
	test := fmt.Sprintf("package feature\n\nimport \"testing\"\n\nfunc TestTaskID(t *testing.T) {\n if TaskID() != %q { t.Fatal(\"unexpected task ID\") }\n}\n", id)
	return change{Files: []fileChange{{Path: dir + "/feature.go", Content: &source}, {Path: dir + "/feature_test.go", Content: &test}}, Summary: "scaffolded " + title, CommitMessage: id + ": scaffold " + title}
}

func validChange(f fileChange) bool {
	return f.Content != nil && fs.ValidPath(f.Path) && f.Path != "." && !strings.Contains(f.Path, "\\") && f.Path != ".git" && !strings.HasPrefix(f.Path, ".git/")
}

func forbidden(err error) bool {
	var status *protocol.HTTPError
	return errors.As(err, &status) && status.Status == http.StatusForbidden
}

func develop(ctx context.Context, j *job) error {
	id := j.text("task_id")
	if id == "" {
		return nil
	}
	title := first(j.text("title"), id)
	key := "iter:" + id
	iteration := 1
	if j.message.Type != "task.ready" {
		if _, err := j.load(ctx, key, &iteration); err != nil {
			return err
		}
		iteration++
	}
	if err := j.save(ctx, key, iteration); err != nil {
		return err
	}
	feedback := append(stringList(j.item["comments"]), stringList(j.item["failures"])...)
	if reason := j.text("reason"); reason != "" {
		feedback = append(feedback, reason)
	}
	escalated := iteration > 4
	if escalated {
		answer, err := j.client.Ask(ctx, fmt.Sprintf("Task %s (%s) has been reworked %d times. Latest feedback: %s. Reply with guidance or 'ship'.", id, title, iteration-1, strings.Join(feedback, "; ")))
		if err != nil {
			return err
		}
		if answer != "" && !strings.EqualFold(strings.TrimSpace(answer), "ship") {
			feedback = append(feedback, answer)
		}
	}
	var research strings.Builder
	paths := stringList(j.item["research"])
	for _, p := range paths {
		file, err := j.client.Tool(ctx, "filesystem.read", map[string]string{"path": p})
		if forbidden(err) || errors.Is(err, protocol.ErrToolFailed) {
			continue
		}
		if err != nil {
			return err
		}
		research.WriteString(file.Content)
		research.WriteByte('\n')
	}
	notes := []rune(research.String())
	prompt := fmt.Sprintf("Implement task %s (%s). Include tests. Return JSON with files (path and content), summary and commit_message.\n%s\nSuggested files: %v\nFeedback: %v\nResearch: %s", id, title, j.text("description"), stringList(j.item["files"]), feedback, string(notes[:min(len(notes), 6000)]))
	reply, err := j.model(ctx, prompt, true)
	if err != nil {
		return err
	}
	var changes change
	parsed := decodeObject(reply, &changes)
	var files []fileChange
	if parsed {
		for _, f := range changes.Files {
			if validChange(f) {
				files = append(files, f)
			}
		}
	}
	if len(files) == 0 {
		changes = scaffold(id, title)
		files = changes.Files
	}
	var written []string
	for _, f := range files {
		if err := j.write(ctx, f.Path, *f.Content); err != nil {
			return err
		}
		written = append(written, f.Path)
	}
	if err := commitFiles(ctx, j.client, written, first(changes.CommitMessage, id+": "+title)); err != nil {
		return err
	}
	item := map[string]any{"request_id": j.text("request_id"), "task_id": id, "title": title, "description": first(j.text("description"), title), "files": written, "summary": first(changes.Summary, "implemented "+title), "branch": j.in.Branch, "iteration": iteration, "research": paths}
	if escalated {
		item["escalated"] = true
	}
	j.emit("code.ready", item)
	j.summary = fmt.Sprintf("%s: wrote %d files (iteration %d)", id, len(written), iteration)
	return nil
}

// A QA retry can produce the same files as the previous turn. Staging is
// idempotent, but committing an unchanged index is an error in Git.
func commitFiles(ctx context.Context, client *protocol.Client, paths []string, message string) error {
	_, err := client.Tool(ctx, "git.add", map[string]any{"paths": paths})
	if forbidden(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, path := range paths {
		diff, err := client.Tool(ctx, "git.diff", map[string]any{"staged": true, "path": path})
		// A node with write-only Git grants retains its ordinary commit path.
		if err != nil && !forbidden(err) {
			return err
		}
		if err == nil && !diff.Truncated && diff.Content == "no differences" {
			continue
		}
		_, err = client.Tool(ctx, "git.commit", map[string]string{"message": message})
		if forbidden(err) {
			return nil
		}
		return err
	}
	return nil
}
