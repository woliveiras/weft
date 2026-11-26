package api

import (
	"cmp"
	"context"
	"errors"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/woliveiras/weft/server/internal/chat"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

const maxPRLookupConnections = 8

type prRepositoryTarget struct {
	connection scm.View
	readers    []manualPRTarget
	reviewers  []manualPRTarget
	starters   []manualPRTarget
}

func (s *Server) prLookupTargets(ctx context.Context, selector chat.PRSelector) ([]prRepositoryTarget, error) {
	locator, valid := selector.NumberAndRepository()
	if !valid || !selector.Valid() {
		return nil, errPRScope
	}
	connections, err := s.SCM.List(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(connections, func(a, b scm.View) int { return cmp.Compare(a.Name, b.Name) })
	repos := make([]prRepositoryTarget, 0, min(len(connections), maxPRLookupConnections+1))
	for _, c := range connections {
		if !matchesPRConnection(c, selector, locator) {
			continue
		}
		repos = append(repos, prRepositoryTarget{connection: c})
		if len(repos) > maxPRLookupConnections {
			break
		}
	}
	if len(repos) == 0 {
		return repos, nil
	}
	workflow := selector.Workflow
	if workflow != "" && selector.WorkflowVersion == 0 {
		selected, err := s.findChatWorkflow(ctx, workflow)
		if errors.Is(err, storage.ErrNotFound) {
			return nil, errPRScope
		}
		if err != nil {
			return nil, err
		}
		workflow = selected.Metadata.ID
	}
	records, err := s.Store.ListGraphRecords(ctx)
	if err != nil {
		return nil, err
	}
	slices.SortFunc(records, func(a, b *domain.GraphRecord) int { return cmp.Compare(a.ID, b.ID) })
	number, _ := strconv.Atoi(locator.Number)
	for _, record := range records {
		if strings.HasPrefix(record.ID, domain.TaskRunGraphPrefix) || workflow != "" && workflow != record.ID ||
			selector.WorkflowVersion > 0 && (selector.WorkflowVersion != record.Version || selector.Run != record.RunID) {
			continue
		}
		g, latest, err := s.Engine.GetGraph(ctx, record.ID, 0)
		if err != nil {
			return nil, err
		}
		if g.Metadata.Labels[domain.LabelTask] != "" || latest.Version != record.Version || g.Metadata.Version != record.Version || latest.RunID != record.RunID {
			continue
		}
		selectedNode := ""
		if selector.Node != "" {
			selectedNode = resolveChatControlNode(g, selector.Node)
			if selectedNode == "" {
				continue
			}
		}
		for i := range g.Spec.Nodes {
			n := &g.Spec.Nodes[i]
			if n.Disabled || selectedNode != "" && n.ID != selectedNode {
				continue
			}
			channel := prReviewerChannel(g, n)
			ready, startable := false, false
			if channel != "" {
				scope, err := s.Engine.WorkflowControlScope(ctx, latest.ID, n.ID)
				if err == nil && scope.RunID == latest.RunID && scope.Version == latest.Version {
					ready = latest.State == domain.GraphRunning && scope.DesiredState == domain.GraphRunning &&
						scope.RunID != "" && !scope.NodePaused && !scope.NodeStopped
					startable = scope.StoppedAndIdle && scope.CanApply("start")
				}
			}
			for j := range repos {
				repo := &repos[j]
				if !supportedPRConnection(repo.connection) {
					continue
				}
				grant, ok := savedPRReadGrant(n.Tools, repo.connection.Name)
				if !ok {
					continue
				}
				c := repo.connection
				target := manualPRTarget{proposal: chat.PRAnalysis{Number: number, Connection: c.Name, ConnectionRevision: c.UpdatedAt.UTC().Format(time.RFC3339Nano),
					Provider: c.Kind, Repository: c.Repository, Graph: latest.ID, Version: latest.Version, Node: n.ID,
					Run: latest.RunID, Channel: channel, ModelProvider: n.Model.Provider, ModelName: n.Model.Name}, grant: grant}
				if ready {
					repo.reviewers = append(repo.reviewers, target)
				} else if len(repo.readers) < 2 {
					repo.readers = append(repo.readers, target)
				}
				if startable {
					repo.starters = append(repo.starters, target)
				}
			}
		}
	}
	return repos, nil
}

func savedPRReadGrant(grants []domain.ToolGrant, connection string) (domain.ToolGrant, bool) {
	for _, grant := range grants {
		if grant.Name == scm.CapPullRequestRead && tools.CheckRepository(grant, connection) == nil && !grant.RequiresApproval {
			return grant, true
		}
	}
	return domain.ToolGrant{}, false
}

func prReviewerChannel(g *domain.Graph, n *domain.Node) string {
	if n.Runtime.Kind != domain.RuntimeNative || n.Loop.Mode != domain.LoopEvent || n.Loop.BatchSize > 1 || len(g.Outgoing(n.ID)) != 0 ||
		n.Model.Provider == "" || n.Model.Name == "" {
		return ""
	}
	ins := g.Incoming(n.ID)
	if len(ins) != 1 || ins[0].From != "" || !slices.Equal(g.Consumers(ins[0]), []string{n.ID}) {
		return ""
	}
	return ins[0].ID
}

func supportedPRConnection(c scm.View) bool {
	return c.TokenConfigured && (c.Kind == "github" || c.Kind == "bitbucket-cloud" || c.Kind == "bitbucket-dc") && safePRConnection(c)
}

func safePRConnection(c scm.View) bool {
	return len(c.Name)+len(c.Repository) <= 160 && chat.ValidateMessage(c.Name) == nil && chat.ValidateMessage(c.Repository) == nil &&
		!strings.ContainsAny(c.Name+c.Repository, "\r\n\t")
}

func matchesPRConnection(c scm.View, selector chat.PRSelector, locator chat.PRLocator) bool {
	if selector.Connection != "" && selector.Connection != c.Name ||
		selector.ConnectionRevision != "" && selector.ConnectionRevision != c.UpdatedAt.UTC().Format(time.RFC3339Nano) ||
		locator.Repository != "" && !strings.EqualFold(locator.Repository, c.Repository) {
		return false
	}
	if locator.Host == "" {
		return true
	}
	if locator.Kind != c.Kind {
		return false
	}
	if git, err := url.Parse(c.GitURL); err == nil && strings.EqualFold(git.Host, locator.Host) &&
		strings.EqualFold(strings.Trim(strings.TrimSuffix(git.Path, ".git"), "/"), strings.Trim(locator.SitePath+"/"+c.Repository, "/")) {
		return true
	}
	api, err := url.Parse(c.APIURL)
	if err != nil {
		return false
	}
	host, path := api.Host, strings.Trim(api.Path, "/")
	switch c.Kind {
	case "github":
		if host == "" || strings.EqualFold(host, "api.github.com") {
			host, path = "github.com", ""
		} else if path == "api/v3" {
			path = ""
		} else {
			path = strings.TrimSuffix(path, "/api/v3")
		}
	case "bitbucket-cloud":
		if host == "" || strings.EqualFold(host, "api.bitbucket.org") {
			host, path = "bitbucket.org", ""
		} else if path == "2.0" {
			path = ""
		} else {
			path = strings.TrimSuffix(path, "/2.0")
		}
	}
	return strings.EqualFold(host, locator.Host) && path == locator.SitePath
}
