package environment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/storage"
)

const generationsNamespace = "environment-generations-v1"

var generationImage = regexp.MustCompile(`^weft-shared/(shared_[a-z0-9]+):ready-user$`)
var reuseID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var ErrInvalidCleanup = errors.New("invalid shared environment cleanup request")

type sharedGeneration struct {
	ID        string    `json:"id"`
	Key       string    `json:"reuseKey"`
	Daemon    string    `json:"daemon"`
	CreatedAt time.Time `json:"createdAt"`
	sharedEnvironment
}
type CleanupItem struct {
	Missing  bool         `json:"missing,omitempty"`
	ID       string       `json:"id"`
	Image    string       `json:"image"`
	Caches   []CacheMount `json:"caches,omitempty"`
	UsedBy   []string     `json:"usedBy,omitempty"`
	Eligible bool         `json:"eligible"`
	Removed  bool         `json:"removed"`
	Error    string       `json:"error,omitempty"`
}
type CleanupResult struct {
	DryRun         bool              `json:"dryRun"`
	Generations    []CleanupItem     `json:"generations"`
	DeferredGraphs []DeferredCleanup `json:"deferredGraphs,omitempty"`
}

func (m *Manager) cleanupDaemon() string {
	host := m.opt.DockerHost
	if host == "" {
		host = os.Getenv("DOCKER_HOST")
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(host)))
}
func validateGeneration(g sharedGeneration) error {
	match := generationImage.FindStringSubmatch(g.Image)
	if len(match) != 2 || match[1] != g.ID || !reuseID.MatchString(g.Key) {
		return fmt.Errorf("invalid shared generation ownership")
	}
	seen := map[string]bool{}
	for _, c := range g.Caches {
		if !shareableCache(c.Path) || c.Volume != CacheVolume(g.ID, c.Path) || seen[c.Volume] {
			return fmt.Errorf("invalid shared cache ownership")
		}
		seen[c.Volume] = true
	}
	return nil
}

// Called under the reuse-key lease, before publication or pointer replacement.
func (m *Manager) trackGeneration(ctx context.Context, key string, s sharedEnvironment) error {
	match := generationImage.FindStringSubmatch(s.Image)
	if len(match) != 2 {
		return fmt.Errorf("invalid shared image ownership")
	}
	g := sharedGeneration{ID: match[1], Key: key, Daemon: m.cleanupDaemon(), CreatedAt: time.Now().UTC(), sharedEnvironment: s}
	if err := validateGeneration(g); err != nil {
		return err
	}
	data, err := m.opt.Store.KVGet(ctx, generationsNamespace, g.ID)
	if err == nil {
		var previous sharedGeneration
		if err = json.Unmarshal(data, &previous); err != nil {
			return err
		}
		if previous.Key != g.Key || previous.Daemon != g.Daemon || previous.Image != g.Image {
			return fmt.Errorf("shared generation identity conflict")
		}
		return nil
	}
	if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	data, err = json.Marshal(g)
	if err != nil {
		return err
	}
	return m.opt.Store.KVPut(ctx, generationsNamespace, g.ID, data)
}

// CleanupShared previews all known generations, or removes explicitly selected
// IDs after rechecking their references under the same lease used by builders.
// No daemon-wide prune, forced removal or workspace/private-volume cleanup.
func (m *Manager) CleanupShared(ctx context.Context, ids []string, dryRun bool, actor string) (result *CleanupResult, err error) {
	if (!dryRun && len(ids) == 0) || len(ids) > 100 {
		return nil, ErrInvalidCleanup
	}
	ctx, span := observability.Start(ctx, "environment.cleanup")
	started := time.Now()
	defer func() {
		outcome := "ok"
		if err != nil {
			outcome = "error"
		}
		if result != nil {
			for _, item := range result.Generations {
				if item.Error != "" {
					outcome = "partial"
				}
			}
		}
		observability.EnvironmentCleanup.WithLabelValues(outcome).Inc()
		observability.EnvironmentCleanupSeconds.Observe(time.Since(started).Seconds())
		spanErr := err
		if outcome == "partial" && spanErr == nil {
			spanErr = errors.New("shared cleanup partially failed")
		}
		observability.EndSpan(span, spanErr)
	}()
	records, err := m.opt.Store.KVList(ctx, generationsNamespace, "")
	if err != nil {
		return nil, err
	}
	selected := map[string]bool{}
	for _, id := range ids {
		if selected[id] || !generationImage.MatchString("weft-shared/"+id+":ready-user") {
			return nil, ErrInvalidCleanup
		}
		selected[id] = true
	}
	keys := make([]string, 0, len(records))
	for id := range selected {
		if records[id] == nil {
			keys = append(keys, id)
		}
	}
	for id := range records {
		if len(ids) == 0 || selected[id] {
			keys = append(keys, id)
		}
	}
	sort.Strings(keys)
	result = &CleanupResult{DryRun: dryRun, Generations: []CleanupItem{}}
	for _, id := range keys {
		if records[id] == nil {
			result.Generations = append(result.Generations, CleanupItem{ID: id, Missing: true})
			continue
		}
		var g sharedGeneration
		if err = json.Unmarshal(records[id], &g); err != nil {
			return result, err
		}
		if g.ID != id {
			return result, fmt.Errorf("shared generation key mismatch")
		}
		if err = validateGeneration(g); err != nil {
			return result, err
		}
		if g.Daemon != m.cleanupDaemon() {
			if selected[id] {
				return result, ErrInvalidCleanup
			}
			continue
		}
		item := CleanupItem{ID: id, Image: g.Image, Caches: g.Caches}
		itemErr := m.withReuseLease(ctx, g.Key, io.Discard, func(ctx context.Context) error {
			// Another cleanup may have completed while this request waited.
			if _, e := m.opt.Store.KVGet(ctx, generationsNamespace, id); errors.Is(e, storage.ErrNotFound) {
				item.Removed = true
				return nil
			} else if e != nil {
				return e
			}
			used, e := m.generationUsers(ctx, g)
			if e != nil {
				return e
			}
			item.UsedBy = used
			item.Eligible = len(used) == 0
			if dryRun || !item.Eligible {
				return nil
			}
			// Invalidate reuse before any destructive step, so a partial failure can
			// never expose a generation whose image or seeds have already been removed.
			data, e := m.opt.Store.KVGet(ctx, reuseNamespace, g.Key)
			if e == nil {
				var current sharedEnvironment
				if e = json.Unmarshal(data, &current); e != nil {
					return e
				}
				if current.Image == g.Image {
					if e = m.opt.Store.KVDelete(ctx, reuseNamespace, g.Key); e != nil {
						return e
					}
				}
			} else if !errors.Is(e, storage.ErrNotFound) {
				return e
			}
			for _, cache := range g.Caches {
				exists, e := m.resourceExists(ctx, "volume", cache.Volume)
				if e != nil {
					return e
				}
				if exists {
					if e = m.docker(ctx, nil, io.Discard, "volume", "rm", cache.Volume); e != nil {
						return fmt.Errorf("remove shared cache: %w", e)
					}
				}
			}
			exists, e := m.resourceExists(ctx, "image", g.Image)
			if e != nil {
				return e
			}
			if exists {
				if e = m.docker(ctx, nil, io.Discard, "rmi", g.Image); e != nil {
					return fmt.Errorf("remove shared image: %w", e)
				}
			}
			if e = m.opt.Store.KVDelete(ctx, generationsNamespace, id); e != nil {
				return e
			}
			item.Removed = true
			return nil
		})
		if itemErr != nil {
			item.Error = itemErr.Error()
			item.Eligible = false
		}
		result.Generations = append(result.Generations, item)
		if !dryRun {
			m.publish(context.WithoutCancel(ctx), "environment.cleanup", "", actor, map[string]any{"generation": id, "removed": item.Removed, "usedBy": item.UsedBy, "error": item.Error})
			if itemErr != nil {
				m.opt.Log.Warn("shared environment cleanup failed", "generation", id, "err", itemErr)
			} else {
				m.opt.Log.Info("shared environment cleanup", "generation", id, "removed", item.Removed)
			}
		}
	}
	return result, nil
}

func (m *Manager) resourceExists(ctx context.Context, kind, name string) (bool, error) {
	var out bytes.Buffer
	format := "{{.Name}}"
	if kind == "image" {
		format = "{{.Id}}"
	}
	err := m.docker(ctx, nil, &out, kind, "inspect", "--format", format, name)
	if err == nil {
		return true, nil
	}
	// Only an explicit absence is safe; daemon/auth/timeouts must fail closed.
	text := strings.ToLower(out.String())
	if strings.Contains(text, "no such image") || strings.Contains(text, "no such volume") || strings.Contains(text, "no such object") {
		return false, nil
	}
	return false, fmt.Errorf("inspect shared %s: %w", kind, err)
}

func (m *Manager) generationUsers(ctx context.Context, g sharedGeneration) ([]string, error) {
	statuses, err := m.opt.Store.KVList(ctx, kvNamespace, "")
	if err != nil {
		return nil, err
	}
	used := []string{}
	for id, data := range statuses {
		var st Status
		if err := json.Unmarshal(data, &st); err != nil {
			return nil, err
		}
		// Older records lack SharedImage: retain every generation of their key.
		if st.SharedImage == g.Image || st.Image == g.Image || (st.SharedImage == "" && st.ReuseKey == g.Key) {
			used = append(used, "graph:"+id)
		}
	}
	graphs, err := m.opt.Store.ListGraphs(ctx)
	if err != nil {
		return nil, err
	}
	for _, graph := range graphs {
		referenced := (graph.Spec.Environment != nil && graph.Spec.Environment.Image == g.Image) || graph.Spec.Defaults.Isolation.Image == g.Image
		if graph.Spec.Environment != nil {
			for _, companion := range graph.Spec.Environment.Companions {
				if companion.Image == g.Image {
					referenced = true
				}
			}
		}
		for _, node := range graph.Spec.Nodes {
			if node.Isolation.Image == g.Image {
				referenced = true
			}
		}
		if referenced {
			used = append(used, "definition:"+graph.Metadata.ID)
		}
	}
	// Read leases after statuses: a rebuild cannot hide the old reference between
	// the two reads. New consumers of this generation wait on our reuse-key lease.
	leases, err := m.opt.Store.ListLeases(ctx, leasePrefix)
	if err != nil {
		return nil, err
	}
	for _, l := range leases {
		if l.ExpiresAt.After(time.Now()) {
			used = append(used, "build:"+strings.TrimPrefix(l.Key, leasePrefix))
		}
	}
	if len(used) > 0 {
		sort.Strings(used)
		return used, nil
	}
	var out bytes.Buffer
	if err := m.docker(ctx, nil, &out, "ps", "-aq"); err != nil {
		return nil, fmt.Errorf("list containers for cleanup: %w", err)
	}
	containers := strings.Fields(out.String())
	if len(containers) == 0 {
		return used, nil
	}
	out.Reset()
	// Inspect only image IDs and named volumes; never environment/credentials.
	args := append([]string{"inspect", "--format", "{{.Id}} {{.Image}}{{range .Mounts}}{{if eq .Type \"volume\"}} {{.Name}}{{end}}{{end}}"}, containers...)
	if err := m.docker(ctx, nil, &out, args...); err != nil {
		return nil, fmt.Errorf("inspect container references: %w", err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	var image bytes.Buffer
	exists, err := m.resourceExists(ctx, "image", g.Image)
	if err != nil {
		return nil, err
	}
	imageID := ""
	if exists {
		if err := m.docker(ctx, nil, &image, "image", "inspect", "--format", "{{.Id}}", g.Image); err != nil {
			return nil, err
		}
		imageID = strings.TrimSpace(image.String())
		if imageID == "" {
			return nil, fmt.Errorf("empty shared image identity")
		}
	}
	volumes := map[string]bool{}
	for _, c := range g.Caches {
		volumes[c.Volume] = true
	}
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return nil, fmt.Errorf("invalid container reference inventory")
		}
		inUse := imageID != "" && fields[1] == imageID
		for _, v := range fields[2:] {
			if volumes[v] {
				inUse = true
			}
		}
		if inUse {
			used = append(used, "container:"+fields[0])
		}
	}
	sort.Strings(used)
	return used, nil
}
