package instructions

import (
	"bufio"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/sys/unix"
	"gopkg.in/yaml.v3"
)

const maxMetadata = 2048

var skillRoots = []struct{ family, dir string }{
	{"agents", ".agents/skills"},
	{"claude", ".claude/skills"},
	{"github", ".github/skills"},
}

var rootInstructions = []struct{ family, file string }{
	{"agents", "AGENTS.md"},
	{"claude", "CLAUDE.md"},
	{"claude", ".claude/CLAUDE.md"},
	{"github", ".github/copilot-instructions.md"},
}

type repoCandidate struct {
	alias, pkg, name, description, metadata, version, lockStatus string
	identity                                                     os.FileInfo
	family                                                       int
}

func safeRelative(rel string) bool {
	if rel == "." || path.Clean(rel) != rel || strings.HasPrefix(rel, "/") || !utf8.ValidString(rel) || strings.ContainsRune(rel, '\\') {
		return false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == ".." || strings.EqualFold(part, ".git") || part == "" {
			return false
		}
	}
	return strings.IndexFunc(rel, unsafeDisplayRune) == -1
}

func unsafeDisplayRune(r rune) bool {
	return unicode.IsControl(r) || unicode.Is(unicode.Cf, r)
}

// Each component is opened relative to a pinned directory, without following
// links. A raced replacement cannot redirect a subsequent open through a link.
func openRepoPath(root *os.File, rel string, directory bool) (*os.File, error) {
	if !safeRelative(rel) {
		return nil, errors.New("unsafe_path")
	}
	parts := strings.Split(rel, "/")
	parent := root
	for i, part := range parts {
		flags := unix.O_RDONLY | unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
		if directory || i < len(parts)-1 {
			flags |= unix.O_DIRECTORY
		}
		// Check the final type before opening it as well as after opening.
		// O_NONBLOCK and the post-open check handle FIFO replacement races.
		if flags&unix.O_DIRECTORY == 0 {
			var info unix.Stat_t
			err := unix.Fstatat(int(parent.Fd()), part, &info, unix.AT_SYMLINK_NOFOLLOW)
			if err != nil || info.Mode&unix.S_IFMT != unix.S_IFREG {
				if parent != root {
					parent.Close()
				}
				if err != nil {
					return nil, err
				}
				return nil, errors.New("unsafe_path")
			}
		}
		fd, err := unix.Openat(int(parent.Fd()), part, flags, 0)
		if parent != root {
			parent.Close()
		}
		if err != nil {
			return nil, err
		}
		parent = os.NewFile(uintptr(fd), part)
	}
	return parent, nil
}

func openRepoRoot(dir string) (*os.File, error) {
	fd, err := unix.Open(dir, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, err
	}
	return os.NewFile(uintptr(fd), dir), nil
}

func fileReason(err error) string {
	switch {
	case errors.Is(err, unix.ENOENT):
		return "missing"
	case errors.Is(err, unix.ELOOP), errors.Is(err, unix.ENOTDIR), err.Error() == "unsafe_path":
		return "unsafe_path"
	default:
		return "unreadable"
	}
}

func regularFile(root *os.File, rel string) (*os.File, string) {
	f, err := openRepoPath(root, rel, false)
	if err != nil {
		return nil, fileReason(err)
	}
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		f.Close()
		return nil, "unsafe_path"
	}
	return f, ""
}

func readRepoText(root *os.File, rel string) (string, string) {
	f, reason := regularFile(root, rel)
	if reason != "" {
		return "", reason
	}
	defer f.Close()
	return readText(f)
}

func readLimitedText(r io.Reader) (string, string) {
	data, err := io.ReadAll(io.LimitReader(r, MaxBytes+1))
	if err != nil {
		return "", "unreadable"
	}
	if len(data) > MaxBytes {
		return "", "size_limit"
	}
	return string(data), ""
}

func readText(r io.Reader) (string, string) {
	body, reason := readLimitedText(r)
	if reason != "" {
		return "", reason
	}
	if err := validBody(body); err != nil {
		return "", err.Error()
	}
	return body, ""
}

func readSkillMetadata(f *os.File) (name, description, metadata, reason string) {
	info, err := f.Stat()
	if err != nil {
		return "", "", "", "unreadable"
	}
	if info.Size() > MaxBytes {
		return "", "", "", "size_limit"
	}
	// Single-byte reads on a bounded header avoid reading into the body during
	// discovery. Read the full file only after the model selects this skill.
	var raw strings.Builder
	reader := bufio.NewReaderSize(singleByteReader{io.LimitReader(f, maxMetadata+1)}, maxMetadata)
	first, err := reader.ReadString('\n')
	if len(first) > maxMetadata {
		return "", "", "", "metadata_limit"
	}
	if err != nil || strings.TrimRight(first, "\r\n") != "---" {
		return "", "", "", "invalid_metadata"
	}
	raw.WriteString(first)
	var header strings.Builder
	for raw.Len() < maxMetadata {
		line, err := reader.ReadString('\n')
		if raw.Len()+len(line) > maxMetadata {
			return "", "", "", "metadata_limit"
		}
		raw.WriteString(line)
		if strings.TrimRight(line, "\r\n") == "---" {
			var fields struct{ Name, Description string }
			if !utf8.ValidString(raw.String()) || yaml.Unmarshal([]byte(header.String()), &fields) != nil ||
				!ID.MatchString(fields.Name) || strings.TrimSpace(fields.Description) == "" ||
				strings.IndexFunc(fields.Description, func(r rune) bool {
					return unsafeDisplayRune(r) && r != '\n' && r != '\t'
				}) != -1 {
				return "", "", "", "invalid_metadata"
			}
			return fields.Name, fields.Description, raw.String(), ""
		}
		if err != nil {
			return "", "", "", "invalid_metadata"
		}
		header.WriteString(line)
	}
	return "", "", "", "metadata_limit"
}

type singleByteReader struct{ r io.Reader }

func (r singleByteReader) Read(p []byte) (int, error) { return r.r.Read(p[:1]) }

func skillFamily(pkg string) int {
	for i, root := range skillRoots {
		if path.Dir(pkg) == root.dir {
			return i
		}
	}
	return -1
}

func aliasTarget(pkg, raw string) string {
	if raw == "" || strings.HasPrefix(raw, "/") || strings.ContainsRune(raw, '\\') {
		return ""
	}
	parts := strings.Split(raw, "/")
	base := strings.Split(path.Dir(pkg), "/")
	// Only leading parent steps are allowed. No unvalidated intermediate
	// directory can be erased by lexical cleaning.
	for len(parts) > 0 && (parts[0] == "." || parts[0] == "..") {
		if parts[0] == ".." {
			if len(base) == 0 {
				return ""
			}
			base = base[:len(base)-1]
		}
		parts = parts[1:]
	}
	target := strings.Join(append(base, parts...), "/")
	if !safeRelative(target) || skillFamily(target) < 0 {
		return ""
	}
	return target
}

func openPackage(root *os.File, candidate string, blocked map[string]bool) (*os.File, string, string) {
	pkg := candidate
	seen := map[string]bool{}
	for len(seen) < 16 {
		family := skillFamily(pkg)
		if family < 0 || !safeRelative(pkg) || seen[pkg] {
			return nil, "", "unsafe_path"
		}
		if blocked[skillRoots[family].family] {
			return nil, "", "blocked_source"
		}
		seen[pkg] = true
		parent, err := openRepoPath(root, path.Dir(pkg), true)
		if err != nil {
			return nil, "", fileReason(err)
		}
		dir, err := openRepoPath(parent, path.Base(pkg), true)
		if err == nil {
			parent.Close()
			return dir, pkg, ""
		}
		var link [4096]byte
		n, linkErr := unix.Readlinkat(int(parent.Fd()), path.Base(pkg), link[:])
		parent.Close()
		if linkErr != nil {
			return nil, "", fileReason(err)
		}
		if n == len(link) {
			return nil, "", "unsafe_path"
		}
		pkg = aliasTarget(pkg, string(link[:n]))
	}
	return nil, "", "unsafe_path"
}

type repoLock struct {
	Version int
	Skills  map[string]struct{ ComputedHash string }
}

func readRepoLock(root *os.File) (repoLock, string) {
	var lock repoLock
	body, reason := readRepoText(root, "skills-lock.json")
	if reason != "" {
		return lock, reason
	}
	if json.Unmarshal([]byte(body), &lock) != nil {
		return repoLock{}, "invalid_lock"
	}
	if lock.Version != 2 {
		return repoLock{}, "unsupported_lock"
	}
	return lock, ""
}

func (l repoLock) declared(name, status string) (string, string) {
	if status != "" {
		return "", status
	}
	record, exists := l.Skills[name]
	if !exists {
		return "", "missing_entry"
	}
	value := record.ComputedHash
	if len(value) != 64 || strings.Trim(value, "0123456789abcdef") != "" {
		return "", "invalid_entry"
	}
	return value, "declared"
}

func discoverSkills(root *os.File, blocked map[string]bool) ([]repoCandidate, []Entry) {
	var candidates []repoCandidate
	var omissions []Entry
	omit := func(source, reason string) {
		omissions = append(omissions, Entry{Scope: "repository-skill", Stage: "catalogue", Source: source, Reason: reason})
	}
	var families [3][]os.DirEntry
	excluded := map[string]bool{}
	for family, denied := range blocked {
		excluded[family] = denied
	}
	for i, family := range skillRoots {
		if blocked[family.family] {
			omit(family.dir, "blocked_source")
			continue
		}
		dir, err := openRepoPath(root, family.dir, true)
		if err != nil {
			omit(family.dir, fileReason(err))
			continue
		}
		entries, err := dir.ReadDir(MaxEntries + 1)
		dir.Close()
		if err != nil && err != io.EOF {
			omit(family.dir, "unreadable")
			excluded[family.family] = true
			continue
		}
		if len(entries) > MaxEntries {
			omit(family.dir, "file_count_limit")
			excluded[family.family] = true
			continue
		}
		families[i] = entries
	}
	for i, family := range skillRoots {
		entries := families[i]
		for _, de := range entries {
			alias := family.dir + "/" + de.Name()
			if !safeRelative(alias) {
				omit(family.dir, "unsafe_path")
				continue
			}
			pkg, canonical, reason := openPackage(root, alias, excluded)
			if reason != "" {
				omit(alias, reason)
				continue
			}
			info, statErr := pkg.Stat()
			file, reason := regularFile(pkg, "SKILL.md")
			pkg.Close()
			if reason != "" || statErr != nil {
				if reason == "" {
					file.Close()
					reason = "unreadable"
				}
				omit(alias, reason)
				continue
			}
			name, description, metadata, reason := readSkillMetadata(file)
			file.Close()
			if reason != "" {
				omit(alias+"/SKILL.md", reason)
				continue
			}
			candidates = append(candidates, repoCandidate{
				alias: alias, pkg: canonical, name: name, description: description, metadata: metadata,
				identity: info, family: skillFamily(canonical),
			})
		}
	}
	if len(candidates) > 0 {
		lock, status := readRepoLock(root)
		for i := range candidates {
			c := &candidates[i]
			c.version, c.lockStatus = lock.declared(c.name, status)
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		a, b := candidates[i], candidates[j]
		if a.family != b.family {
			return a.family < b.family
		}
		if a.pkg != b.pkg {
			return a.pkg < b.pkg
		}
		if (a.alias == a.pkg) != (b.alias == b.pkg) {
			return a.alias == a.pkg
		}
		return a.alias < b.alias
	})
	return candidates, omissions
}
