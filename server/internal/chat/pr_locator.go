package chat

import (
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

var prRepository = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,127}/[A-Za-z0-9][A-Za-z0-9_.-]{0,127}$`)

type PRLocator struct {
	Kind       string
	Repository string
	Host       string
	Number     string
	SitePath   string
}

func ReadPRURL(raw string) (PRLocator, bool) {
	var location PRLocator
	if !safeText(raw, 512) {
		return location, false
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.RawQuery != "" || u.RawPath != "" || u.Fragment != "" {
		return location, false
	}
	path := strings.Trim(u.Path, "/")
	if path == "" || strings.Contains(path, "//") {
		return location, false
	}
	parts := strings.Split(path, "/")
	for _, part := range parts {
		if part == "." || part == ".." {
			return location, false
		}
	}
	if end := parts[len(parts)-1]; end == "files" || end == "commits" || end == "overview" || end == "diff" {
		parts = parts[:len(parts)-1]
	}
	n := len(parts)
	switch {
	case n >= 4 && parts[n-2] == "pull":
		location.Kind = "github"
		location.Repository = parts[n-4] + "/" + parts[n-3]
		location.SitePath = strings.Join(parts[:n-4], "/")
	case n >= 4 && parts[n-2] == "pull-requests" && (n < 6 || parts[n-6] != "projects" || parts[n-4] != "repos"):
		location.Kind = "bitbucket-cloud"
		location.Repository = parts[n-4] + "/" + parts[n-3]
		location.SitePath = strings.Join(parts[:n-4], "/")
	case n >= 6 && parts[n-6] == "projects" && parts[n-4] == "repos" && parts[n-2] == "pull-requests":
		location.Kind = "bitbucket-dc"
		location.Repository = parts[n-5] + "/" + parts[n-3]
		location.SitePath = strings.Join(parts[:n-6], "/")
	default:
		return location, false
	}
	location.Host, location.Number = strings.ToLower(u.Host), parts[n-1]
	number, err := strconv.Atoi(location.Number)
	return location, err == nil && number > 0 && strconv.Itoa(number) == location.Number && prRepository.MatchString(location.Repository)
}

func (p PRSelector) NumberAndRepository() (PRLocator, bool) {
	if p.URL != "" {
		locator, ok := ReadPRURL(p.URL)
		return locator, ok && (p.Number == "" || p.Number == locator.Number) &&
			(p.Repository == "" || strings.EqualFold(p.Repository, locator.Repository))
	}
	locator := PRLocator{Repository: p.Repository, Number: p.Number}
	number, err := strconv.Atoi(p.Number)
	return locator, err == nil && number > 0 && strconv.Itoa(number) == p.Number && (p.Repository == "" || prRepository.MatchString(p.Repository))
}

func SafePRTitle(value string) string {
	if !safeText(value, 512) {
		return ""
	}
	value = strings.Join(strings.Fields(value), " ")
	if chars := []rune(value); len(chars) > 104 {
		value = string(chars[:104]) + "…"
	}
	return value
}
