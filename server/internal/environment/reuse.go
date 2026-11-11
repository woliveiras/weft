package environment

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/storage"
)

const reuseNamespace = "environment-reuse-v1"

type sharedEnvironment struct {
	Image  string       `json:"image"`
	Caches []CacheMount `json:"caches,omitempty"`
}

// Exact repository identity deliberately avoids merging different transports or
// credentials. Records are scoped to this installation's store and daemon.
func reuseKey(repo, definition, inputs, platform, daemon string) string {
	data, _ := json.Marshal([]string{"reuse-v1-known-download-cache-copies", repo, definition, inputs, platform, daemon})
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func reuseScope(repo, daemon string) string { return reuseKey(repo, "", "", "", daemon) }

// withReuseLease serializes publication and avoids duplicate concurrent builds.
// A lost renewal cancels Docker work before another owner can publish its result.
func (m *Manager) withReuseLease(ctx context.Context, key string, log io.Writer, fn func(context.Context) error) (err error) {
	holder := domain.NewID("reuse")
	lease := "environment-reuse/" + key
	waiting := false
	for {
		ok, e := m.opt.Store.AcquireLease(ctx, lease, holder, leaseTTL)
		if e != nil {
			return e
		}
		if ok {
			setProgress(ctx, "preparing", "")
			break
		}
		if !waiting {
			setProgress(ctx, "waiting_reuse", "")
			fmt.Fprintln(log, "==> Waiting for compatible environment build")
			waiting = true
		}
		timer := time.NewTimer(100 * time.Millisecond)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
	child, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(leaseTTL / 3)
		defer ticker.Stop()
		for {
			select {
			case <-child.Done():
				done <- nil
				return
			case <-ticker.C:
				ok, e := m.opt.Store.AcquireLease(child, lease, holder, leaseTTL)
				if e != nil || !ok {
					cancel()
					done <- fmt.Errorf("shared build lease lost: %v", e)
					return
				}
			}
		}
	}()
	err = fn(child)
	cancel()
	err = errors.Join(err, <-done)
	cleanup, stop := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer stop()
	return errors.Join(err, m.opt.Store.ReleaseLease(cleanup, lease, holder))
}

func (m *Manager) sharedSteps(ctx context.Context, g *domain.Graph, st *Status, force bool, log io.Writer) (image string, caches []CacheMount, err error) {
	if graph.RepoKind(g.Spec.Workspace.Repo) == graph.RepoInvalid {
		return "", nil, fmt.Errorf("reuse requires a remote repository identity")
	}
	// Explicitly pointing two graphs at one directory would defeat task isolation.
	if g.Spec.Workspace.Path != "" {
		return "", nil, fmt.Errorf("repository environment reuse requires the default isolated graph workspace (omit workspace.path)")
	}
	dir, err := m.opt.Workspaces.Ensure(ctx, g)
	if err != nil {
		return "", nil, err
	}
	evidence, err := readManifests(ctx, dir)
	if err != nil {
		return "", nil, err
	}
	var platform bytes.Buffer
	if err = m.docker(ctx, nil, &platform, "version", "--format", "{{.Server.Os}}/{{.Server.Arch}}"); err != nil {
		return "", nil, fmt.Errorf("read sandbox platform: %w", err)
	}
	if strings.TrimSpace(platform.String()) == "" {
		return "", nil, fmt.Errorf("sandbox platform is unavailable")
	}
	st.Inputs = evidence.SHA256
	st.Scope = reuseScope(g.Spec.Workspace.Repo, m.opt.DockerHost)
	st.ReuseKey = reuseKey(g.Spec.Workspace.Repo, definitionDigest(g.Spec.Environment), st.Inputs, strings.TrimSpace(platform.String()), m.opt.DockerHost)
	err = m.withReuseLease(ctx, st.ReuseKey, log, func(ctx context.Context) (buildErr error) {
		var shared sharedEnvironment
		data, e := m.opt.Store.KVGet(ctx, reuseNamespace, st.ReuseKey)
		if e != nil && !errors.Is(e, storage.ErrNotFound) {
			return e
		}
		// Retain the previous generation before a force rebuild replaces the pointer.
		if e == nil {
			var previous sharedEnvironment
			if err := json.Unmarshal(data, &previous); err != nil {
				return err
			}
			if err := m.trackGeneration(ctx, st.ReuseKey, previous); err != nil {
				return err
			}
		}
		hit := e == nil && !force
		published := hit
		defer func() {
			if published || shared.Image == "" {
				return
			}
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), time.Minute)
			defer cancel()
			for _, c := range shared.Caches {
				buildErr = errors.Join(buildErr, m.docker(cleanup, nil, io.Discard, "volume", "rm", c.Volume))
			}
			buildErr = errors.Join(buildErr, m.docker(cleanup, nil, io.Discard, "rmi", shared.Image))
		}()
		if hit {
			setProgress(ctx, "preparing", "compatible")
			if e = json.Unmarshal(data, &shared); e != nil {
				return fmt.Errorf("read shared environment: %w", e)
			}
			if shared.Image == "" {
				return fmt.Errorf("shared environment has no image; force rebuild")
			}
			if e = m.docker(ctx, nil, io.Discard, "image", "inspect", shared.Image); e != nil {
				return fmt.Errorf("shared image unavailable; force rebuild: %w", e)
			}
			for _, c := range shared.Caches {
				if e = m.docker(ctx, nil, io.Discard, "volume", "inspect", c.Volume); e != nil {
					return fmt.Errorf("shared cache unavailable; force rebuild: %w", e)
				}
			}
			fmt.Fprintln(log, "==> Reusing compatible prepared image and immutable cache snapshots")
		} else {
			decision := "no_compatible_generation"
			if force {
				decision = "forced"
			}
			setProgress(ctx, "building_image", decision)
			fmt.Fprintln(log, "==> Preparing shared image (new inputs or explicit rebuild)")
			// Unique names prevent force rebuilds replacing images or caches in use.
			generation := domain.NewID("shared")
			final := "weft-shared/" + generation + ":ready"
			planned := sharedEnvironment{Image: final + "-user"}
			for _, path := range g.Spec.Environment.Caches {
				if shareableCache(path) {
					planned.Caches = append(planned.Caches, CacheMount{Path: path, Volume: CacheVolume(generation, path)})
				}
			}
			// Persist ownership before Docker work, including resources that a
			// crash or partial build may leave behind. Missing resources are
			// handled idempotently by explicit cleanup.
			if e := m.trackGeneration(ctx, st.ReuseKey, planned); e != nil {
				return e
			}
			prepared, e := m.prepareImage(ctx, g, final, log)
			if e != nil {
				return e
			}
			shared.Image = prepared
			for _, cache := range planned.Caches {
				if e := m.docker(ctx, nil, io.Discard, "volume", "create", "--label", "weft.shared="+st.ReuseKey, cache.Volume); e != nil {
					return e
				}
				shared.Caches = append(shared.Caches, cache)
			}
		}
		if err := m.trackGeneration(ctx, st.ReuseKey, shared); err != nil {
			return err
		}
		st.SharedImage = shared.Image
		st.Reused = hit
		setProgress(ctx, "setup", "")
		// Each graph gets private writable volumes. Published seeds are only read.
		mounts := shared.Caches
		if hit {
			mounts, e = m.copySharedCaches(ctx, g.Metadata.ID, shared, log)
			if e != nil {
				return e
			}
		}
		var private []CacheMount
		for _, path := range g.Spec.Environment.Caches {
			if shareableCache(path) {
				continue
			}
			volume := CacheVolume(g.Metadata.ID+"-"+domain.NewID("cache"), path)
			if e = m.docker(ctx, nil, io.Discard, "volume", "create", "--label", "weft.graph="+g.Metadata.ID, volume); e != nil {
				return e
			}
			private = append(private, CacheMount{Path: path, Volume: volume})
		}
		if len(private) > 0 {
			fmt.Fprintln(log, "==> Keeping unrecognized cache directories task-private")
		}
		setupMounts := append(append([]CacheMount(nil), mounts...), private...)
		image, e = m.setupSharedWorkspace(ctx, g, dir, shared.Image, setupMounts, log)
		if e != nil {
			return e
		}
		current, e := readManifests(ctx, dir)
		if e != nil {
			return e
		}
		if current.SHA256 != evidence.SHA256 {
			return fmt.Errorf("setup inputs changed during build; build again")
		}
		if !hit {
			// Publish only after successful setup. Its cache volumes become immutable;
			// arbitrary files in the setup container never enter the shared image.
			data, e = json.Marshal(shared)
			if e != nil {
				return e
			}
			if e = m.opt.Store.KVPut(ctx, reuseNamespace, st.ReuseKey, data); e != nil {
				return e
			}
			published = true
			mounts, e = m.copySharedCaches(ctx, g.Metadata.ID, shared, log)
			if e != nil {
				return e
			}
		}
		caches = append(mounts, private...)
		return nil
	})
	return image, caches, err
}

func (m *Manager) copySharedCaches(ctx context.Context, id string, shared sharedEnvironment, log io.Writer) ([]CacheMount, error) {
	var result []CacheMount
	// New volumes on every rebuild: never overwrite a running task's cache.
	generation := domain.NewID("cache")
	for _, seed := range shared.Caches {
		volume := CacheVolume(id+"-"+generation, seed.Path)
		if err := m.docker(ctx, nil, io.Discard, "volume", "create", "--label", "weft.graph="+id, volume); err != nil {
			return nil, err
		}
		// Destination uses its original image path for Docker's uid-10001 copy-up.
		args := []string{"run", "--rm", "--network", "none", "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "10001", "-v", seed.Volume + ":/tmp/weft-cache-seed:ro", "-v", volume + ":" + seed.Path, "--entrypoint", "sh", shared.Image, "-c", "cp -R /tmp/weft-cache-seed/. " + shellQuote(seed.Path) + "/"}
		if err := m.docker(ctx, nil, log, args...); err != nil {
			return nil, fmt.Errorf("copy compatible cache: %w", err)
		}
		result = append(result, CacheMount{Path: seed.Path, Volume: volume})
	}
	return result, nil
}

func (m *Manager) setupSharedWorkspace(ctx context.Context, g *domain.Graph, dir, prepared string, caches []CacheMount, log io.Writer) (string, error) {
	env := g.Spec.Environment
	// Initialize cache ownership from the image even when there is no setup.
	if len(env.Setup) == 0 && len(caches) == 0 {
		return prepared, nil
	}
	name := buildContainer(g.Metadata.ID)
	args := []string{"run", "--name", name, "--network", SetupNetwork(env), "--cap-drop", "ALL", "--security-opt", "no-new-privileges", "--user", "10001", "-e", "CI=true", "--label", "weft.graph=" + g.Metadata.ID, "-v", m.opt.Workspaces.HostPath(dir) + ":/workspace", "-w", "/workspace"}
	for _, c := range caches {
		args = append(args, "-v", c.Volume+":"+c.Path)
	}
	if env.CPU != "" {
		args = append(args, "--cpus", env.CPU)
	}
	if env.Memory != "" {
		args = append(args, "--memory", env.Memory)
	}
	args = append(args, "--entrypoint", "sh", prepared, "-c", SetupScript(env.Setup))
	fmt.Fprintln(log, "==> Initializing isolated workspace and writable caches")
	if err := m.docker(ctx, nil, log, args...); err != nil {
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
		defer cancel()
		return "", errors.Join(err, m.docker(cleanup, nil, io.Discard, "rm", "-f", name))
	}
	final := prepared
	var err error
	if len(env.Setup) > 0 {
		final = ImageRef(g.Metadata.ID, Hash(env))
		err = m.docker(ctx, nil, log, "commit", name, final)
	}
	cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	err = errors.Join(err, m.docker(cleanup, nil, io.Discard, "rm", "-f", name))
	return final, err
}

// Only directories with a dependency-download/build-cache contract become seeds.
// Broad HOME/.cache directories, credential directories and unknown paths stay private.
func shareableCache(dir string) bool {
	switch path.Clean(dir) {
	case "/home/sandbox/.npm/_cacache", "/home/sandbox/.cache/pip", "/home/sandbox/go/pkg/mod", "/home/sandbox/.cache/go-build", "/home/sandbox/.local/share/pnpm/store", "/home/sandbox/.pnpm-store":
		return true
	default:
		return false
	}
}
