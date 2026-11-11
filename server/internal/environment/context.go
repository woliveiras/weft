package environment

import (
	"archive/tar"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	"github.com/moby/patternmatcher"
	"github.com/moby/patternmatcher/ignorefile"
	"github.com/woliveiras/weft/server/internal/domain"
)

const (
	maxContextBytes   int64 = 64 << 20
	maxContextEntries       = 10000
	contextFiles            = "context"
	contextDockerfile       = ".weft-build.Dockerfile"
)

// ContextSnapshot is immutable input to Docker, spooled to a private temporary
// file rather than held in memory or mounted from the live workspace.
type ContextSnapshot struct {
	File   *os.File
	SHA256 string
}

func (s *ContextSnapshot) Close() {
	name := s.File.Name()
	_ = s.File.Close()
	_ = os.Remove(name)
}

func SnapshotContext(ctx context.Context, dir string, env *domain.EnvironmentSpec) (*ContextSnapshot, error) {
	return snapshotContext(ctx, dir, env, maxContextBytes, maxContextEntries)
}

func snapshotContext(ctx context.Context, dir string, env *domain.EnvironmentSpec, byteLimit int64, entryLimit int) (snapshot *ContextSnapshot, err error) {
	if !fsLocal(env.Context) || env.Dockerfile == "" {
		return nil, fmt.Errorf("invalid repository build context")
	}
	buildfile, err := contextBuildfile(env.Dockerfile)
	if err != nil {
		return nil, err
	}
	if int64(len(buildfile)) > byteLimit {
		return nil, fmt.Errorf("build context exceeds %d bytes", byteLimit)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("open context workspace: %w", err)
	}
	defer root.Close()
	// Do not follow symlinks in any context directory component.
	parts := strings.Split(path.Clean(env.Context), "/")
	for i := range parts {
		info, e := root.Lstat(path.Join(parts[:i+1]...))
		if e != nil {
			return nil, fmt.Errorf("inspect context: %w", e)
		}
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("context must be a directory without symlinks")
		}
	}
	contextRoot, err := root.OpenRoot(env.Context)
	if err != nil {
		return nil, err
	}
	defer contextRoot.Close()
	ignorePath := path.Join(env.Context, ".dockerignore")
	if env.Origin != nil && env.Origin.DockerfilePath != "" {
		specific := env.Origin.DockerfilePath + ".dockerignore"
		if _, e := root.Lstat(specific); e == nil {
			ignorePath = specific
		} else if !os.IsNotExist(e) {
			return nil, fmt.Errorf("inspect Dockerfile ignore file: %w", e)
		}
	}
	var patterns []string
	var ignoreData []byte
	if _, e := root.Lstat(ignorePath); e == nil {
		data, e := readDefinition(root, ignorePath)
		if e != nil {
			return nil, fmt.Errorf("read ignore rules: %w", e)
		}
		ignoreData = data
		patterns, e = ignorefile.ReadAll(strings.NewReader(string(data)))
		if e != nil {
			return nil, fmt.Errorf("invalid ignore rules: %w", e)
		}
	} else if !os.IsNotExist(e) {
		return nil, fmt.Errorf("inspect ignore rules: %w", e)
	}
	matcher, err := patternmatcher.New(patterns)
	if err != nil {
		return nil, fmt.Errorf("invalid ignore patterns: %w", err)
	}
	file, err := os.CreateTemp("", "weft-context-*.tar")
	if err != nil {
		return nil, err
	}
	snapshot = &ContextSnapshot{File: file}
	owned := snapshot
	defer func() {
		if err != nil {
			owned.Close()
			snapshot = nil
		}
	}()
	hash := sha256.New()
	fmt.Fprintf(hash, "context-v1\n%s\n%x\n", ignorePath, sha256.Sum256(ignoreData))
	archive := tar.NewWriter(io.MultiWriter(file, hash))
	if err = archive.WriteHeader(&tar.Header{Name: contextFiles + "/", Mode: 0755, Typeflag: tar.TypeDir}); err != nil {
		return nil, err
	}
	total := int64(len(buildfile))
	count := 0
	err = fs.WalkDir(contextRoot.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if e := ctx.Err(); e != nil {
			return e
		}
		if walkErr != nil {
			return walkErr
		}
		if name == "." {
			return nil
		}
		count++
		if count > entryLimit {
			return fmt.Errorf("build context exceeds %d entries", entryLimit)
		}
		// These installation protections cannot be undone by ignore negations.
		base := entry.Name()
		if base == ".git" || base == ".weft" || base == ".env" || strings.HasPrefix(base, ".env.") {
			if entry.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		// Ignore control files are not build inputs available to COPY/ADD. Their
		// rules are already applied here. Generated controls are outside COPY inputs.
		if name == ".dockerignore" || path.Join(env.Context, name) == ignorePath {
			return nil
		}
		ignored, e := matcher.MatchesOrParentMatches(name)
		if e != nil {
			return e
		}
		if ignored {
			return nil
		} // still traverse directories for later ! exceptions
		info, e := entry.Info()
		if e != nil {
			return e
		}
		if !info.IsDir() && !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported context entry %q: only regular files and directories are allowed", name)
		}
		header := &tar.Header{Name: contextFiles + "/" + name, Mode: int64(info.Mode().Perm()), Typeflag: tar.TypeReg, Size: info.Size()}
		if info.IsDir() {
			header.Typeflag = tar.TypeDir
			header.Size = 0
			header.Name += "/"
		}
		if header.Size > byteLimit-total {
			return fmt.Errorf("build context exceeds %d bytes", byteLimit)
		}
		total += header.Size
		if e := archive.WriteHeader(header); e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		input, e := contextRoot.Open(name)
		if e != nil {
			return e
		}
		defer input.Close()
		current, e := input.Stat()
		if e != nil {
			return e
		}
		if !current.Mode().IsRegular() || !os.SameFile(current, info) || current.Size() != info.Size() {
			return fmt.Errorf("context file %q changed while reading", name)
		}
		if _, e := io.CopyN(archive, &contextReader{ctx: ctx, reader: input}, header.Size); e != nil {
			return fmt.Errorf("read context file %q: %w", name, e)
		}
		var extra [1]byte
		if n, e := input.Read(extra[:]); n != 0 || e != io.EOF {
			return fmt.Errorf("context file %q changed while reading", name)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err = archive.WriteHeader(&tar.Header{Name: contextDockerfile, Mode: 0600, Typeflag: tar.TypeReg, Size: int64(len(buildfile))}); err != nil {
		return nil, err
	}
	if _, err = io.WriteString(archive, buildfile); err != nil {
		return nil, err
	}
	if err = archive.Close(); err != nil {
		return nil, err
	}
	if _, err = file.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	snapshot.SHA256 = hex.EncodeToString(hash.Sum(nil))
	return snapshot, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

// Tar stdin is a remote context: Docker does not apply client-side ignore rules.
// Place filtered files below a separate root and scope local sources to it so
// COPY . cannot include the generated build definition or any control files.
func contextBuildfile(source string) (string, error) {
	if err := validateRepositoryDockerfile(source); err != nil {
		return "", err
	}
	var out strings.Builder
	pending := ""
	for _, raw := range strings.Split(source, "\n") {
		line := strings.TrimSpace(raw)
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			pending += strings.TrimSuffix(line, "\\") + " "
			continue
		}
		line = pending + line
		pending = ""
		fields := strings.Fields(line)
		if len(fields) == 0 {
			continue
		}
		instruction := strings.ToUpper(fields[0])
		if instruction == "COPY" || instruction == "ADD" {
			from, operands, err := parseCopy(instruction, strings.TrimSpace(strings.TrimPrefix(line, fields[0])))
			if err != nil {
				return "", err
			}
			if from == "" {
				for i := 0; i < len(operands)-1; i++ {
					operands[i] = contextFiles + "/" + operands[i]
				}
			}
			encoded, err := json.Marshal(operands)
			if err != nil {
				return "", err
			}
			line = instruction + " "
			if from != "" {
				line += "--from=" + from + " "
			}
			line += string(encoded)
		}
		out.WriteString(line + "\n")
	}
	return out.String(), nil
}
