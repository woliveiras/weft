package graph

import (
	"regexp"
	"strings"
)

// RepoURLKind classifies spec.workspace.repo values.
type RepoURLKind int

const (
	// RepoInvalid is anything the platform refuses to clone: local paths,
	// file://, git's ext:: and other transports, or option-like values.
	RepoInvalid RepoURLKind = iota
	// RepoRemote is https://, ssh://, git:// or scp-like user@host:path.
	RepoRemote
	// RepoInsecure is plain http:// (allowed with a warning).
	RepoInsecure
)

// scp-like syntax git accepts for ssh: user@host:path (no scheme, no slash
// before the colon).
var scpLike = regexp.MustCompile(`^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:[^/\\][^\s]*$|^[A-Za-z0-9._-]+@[A-Za-z0-9.-]+:/[^\s]+$`)

// RepoKind decides whether a repository URL points at a remote host.
// Cloning a local path would let a graph copy another graph's workspace or
// the platform's own files into its workspace, so only network transports
// are accepted.
func RepoKind(repo string) RepoURLKind {
	repo = strings.TrimSpace(repo)
	if repo == "" || strings.HasPrefix(repo, "-") || strings.ContainsAny(repo, " \t\n") {
		return RepoInvalid
	}
	lower := strings.ToLower(repo)
	for _, scheme := range []string{"https://", "ssh://", "git://"} {
		if strings.HasPrefix(lower, scheme) && len(repo) > len(scheme) {
			return RepoRemote
		}
	}
	if strings.HasPrefix(lower, "http://") && len(repo) > len("http://") {
		return RepoInsecure
	}
	if strings.Contains(repo, "://") || strings.Contains(repo, "::") {
		return RepoInvalid // file://, ext::, fd::, ...
	}
	if scpLike.MatchString(repo) {
		return RepoRemote
	}
	return RepoInvalid
}
