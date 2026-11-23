package scm

import (
	"strings"
	"testing"
)

func TestSecurityGroupingIdentityAndOrder(t *testing.T) {
	c := Connection{Name: "team", Repository: "acme/app", BranchPrefix: "automation/"}
	a := SecurityAlert{Number: 2, Package: "undici", Ecosystem: "npm", Manifest: "web/pnpm-lock.yaml", Severity: "high", GHSA: "GHSA-fixture", VulnerableRange: "< 8.12.2", FirstPatchedVersion: "8.12.2"}
	b := a
	b.Number = 8
	branch := securityBranch(c, "main", a)
	if securityBranch(c, "main", b) != branch {
		t.Fatal("alert number changes branch")
	}
	prs := []securityPR{{Number: 4, Head: branch, Base: "main", HeadRepository: "acme/app", BaseRepository: "acme/app", URL: "https://github.com/acme/app/pull/4"}, {Number: 5, Head: branch, Base: "main", HeadRepository: "fork/app", BaseRepository: "acme/app"}, {Number: 6, Head: branch, Base: "release", HeadRepository: "acme/app", BaseRepository: "acme/app"}}
	groups, err := groupSecurityAlerts(c, "main", []SecurityAlert{b, a}, prs)
	if err != nil || len(groups) != 1 || groups[0].Branch != branch || len(groups[0].Alerts) != 2 || groups[0].Alerts[0].Number != 2 || len(groups[0].ExistingPullRequests) != 1 || groups[0].ExistingPullRequests[0].Number != 4 {
		t.Fatalf("groups=%+v err=%v", groups, err)
	}
	reversed, reverseErr := groupSecurityAlerts(c, "main", []SecurityAlert{a, b}, prs)
	if reverseErr != nil || len(reversed) != 1 || reversed[0].Branch != branch {
		t.Fatal("ordering changes branch", reversed, reverseErr)
	}
	if !strings.HasPrefix(branch, c.Prefix()+"security-") || !validBranch(branch) {
		t.Fatal("invalid stable branch", branch)
	}
	for _, changed := range []SecurityAlert{{Package: "other", Ecosystem: a.Ecosystem, Manifest: a.Manifest}, {Package: a.Package, Ecosystem: "pip", Manifest: a.Manifest}, {Package: a.Package, Ecosystem: a.Ecosystem, Manifest: "other/lock"}} {
		if securityBranch(c, "main", changed) == branch {
			t.Fatal("different dependency reused branch")
		}
	}
	if securityBranch(c, "release", a) == branch {
		t.Fatal("base branch not included in identity")
	}
	groups, err = groupSecurityAlerts(c, "main", nil, nil)
	if err != nil || groups == nil || len(groups) != 0 {
		t.Fatal("empty inventory", groups, err)
	}
}

func TestSecurityGroupingRejectsIncompleteData(t *testing.T) {
	a := SecurityAlert{Number: 1, Package: "undici", Ecosystem: "npm", Manifest: "web/pnpm-lock.yaml", GHSA: "GHSA-fixture", Severity: "medium", VulnerableRange: "<8"}
	for _, mode := range []string{"number", "package", "ecosystem", "manifest", "traversal", "severity", "advisory", "range", "duplicate", "oversized"} {
		t.Run(mode, func(t *testing.T) {
			v := a
			switch mode {
			case "number":
				v.Number = 0
			case "package":
				v.Package = ""
			case "ecosystem":
				v.Ecosystem = ""
			case "manifest":
				v.Manifest = ""
			case "traversal":
				v.Manifest = "../lock"
			case "severity":
				v.Severity = "unknown"
			case "advisory":
				v.GHSA = ""
			case "range":
				v.VulnerableRange = ""
			case "oversized":
				v.Package = strings.Repeat("a", 2049)
			}
			alerts := []SecurityAlert{v}
			if mode == "duplicate" {
				alerts = append(alerts, v)
			}
			if groups, err := groupSecurityAlerts(Connection{}, "main", alerts, nil); err == nil || groups != nil {
				t.Fatal("incomplete inventory accepted", groups, err)
			}
		})
	}
}
