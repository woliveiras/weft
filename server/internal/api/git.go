package api

import (
	"fmt"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/projectconfig"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/workspace"
)

func (s *Server) inspectGit(w http.ResponseWriter, r *http.Request) error {
	root, err := s.browseRoot(r)
	if err != nil {
		return err
	}
	view, path := r.PathValue("view"), r.URL.Query().Get("path")
	if _, err := workspace.InspectionArgs(view, path); err != nil {
		return badRequest("%v", err)
	}
	if _, _, err := browsePath(root, path); err != nil {
		return badRequest("invalid workspace path")
	}
	g, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		return err
	}
	base, err := s.Workspaces.GraphDir(g)
	if err != nil {
		return err
	}
	var reviewedBase []string
	if strings.HasPrefix(g.Metadata.ID, taskrun.GraphPrefix) {
		task, e := (taskconfig.Manager{Store: s.Store}).Get(r.Context(), g.Metadata.Labels["weft.task"])
		if e != nil {
			return e
		}
		if !validInspectionTask(g, task) {
			return badRequest("invalid task workspace binding")
		}
		if len(task.Repositories) > 0 {
			alias := r.URL.Query().Get("repository")
			if alias == "" {
				return badRequest("select a repository for Git inspection")
			}
			var projectID string
			for _, repo := range task.Repositories {
				if repo.Alias == alias {
					projectID = repo.ProjectID
					reviewedBase = []string{repo.BaseCommit}
					break
				}
			}
			if projectID == "" || root != base {
				return badRequest("unknown repository or incompatible replica workspace")
			}
			project, e := (projectconfig.Manager{Store: s.Store}).Get(r.Context(), projectID)
			if e != nil {
				return e
			}
			taskRoot, e := filepath.EvalSymlinks(s.Workspaces.Root)
			if e != nil {
				return e
			}
			root = filepath.Join(taskRoot, g.Metadata.ID, alias)
			base, e = (projectconfig.Manager{Store: s.Store}).TaskInspectionBase(r.Context(), project, g.Metadata.ID, root, alias)
			if e != nil {
				return e
			}
			if _, _, e = browsePath(root, path); e != nil {
				return badRequest("invalid repository path")
			}
		} else if task.ProjectID != "" {
			expected, e := filepath.EvalSymlinks(filepath.Join(s.Workspaces.Root, g.Metadata.ID))
			actual, actualErr := filepath.EvalSymlinks(base)
			if e != nil || actualErr != nil || expected != actual {
				return badRequest("task workspace path changed")
			}
			project, e := (projectconfig.Manager{Store: s.Store}).Get(r.Context(), task.ProjectID)
			if e != nil {
				return e
			}
			canonical, e := filepath.EvalSymlinks(project.Path)
			if e != nil || canonical != project.Path {
				return badRequest("registered project path is unavailable or changed")
			}
			base, e = (projectconfig.Manager{Store: s.Store}).TaskInspectionBase(r.Context(), project, g.Metadata.ID, actual)
			if e != nil {
				return e
			}
		}
	}
	result, err := s.Workspaces.Inspect(r.Context(), root, base, view, path, reviewedBase...)
	if err != nil {
		return err
	}
	result.GraphID = g.Metadata.ID
	result.NodeID = r.URL.Query().Get("node")
	if result.NodeID != "" {
		rep, _ := strconv.Atoi(r.URL.Query().Get("replica"))
		result.Replica = &rep
	}
	writeJSON(w, 200, result)
	return nil
}

// browsePath retains the existing absolute-within-workspace contract while
// keeping repository internals private, including aliases through symlinks.
func browsePath(root, path string) (string, string, error) {
	for _, part := range strings.Split(filepath.ToSlash(path), "/") {
		if part == ".." || strings.EqualFold(part, ".git") {
			return "", "", fmt.Errorf("invalid workspace path")
		}
	}
	abs, rel, err := tools.ResolvePath(root, path)
	if err != nil {
		return "", "", err
	}
	for _, part := range strings.Split(rel, "/") {
		if strings.EqualFold(part, ".git") {
			return "", "", fmt.Errorf("repository internals are private")
		}
	}
	return abs, rel, nil
}

// Resolve project metadata only through the persisted admission binding. A
// repository-controlled .git file or graph label cannot choose another project.
func validInspectionTask(g *domain.Graph, d *taskconfig.Draft) bool {
	return d != nil && g.Metadata.ID == taskrun.ExecutionID(d.ID) && d.ExecutionGraphID == g.Metadata.ID && g.Metadata.Labels["weft.task"] == d.ID && g.Metadata.Labels["weft.workflow"] == d.GraphID
}

func (s *Server) publishReplicaBranch(w http.ResponseWriter, r *http.Request) error {
	replica, err := strconv.Atoi(r.PathValue("replica"))
	if err != nil || replica < 0 {
		return badRequest("invalid replica index")
	}
	req, err := workspace.DecodeBranchPublication(http.MaxBytesReader(w, r.Body, 1024))
	if err != nil {
		return badRequest("%v", err)
	}
	g, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		return err
	}
	node := r.PathValue("node")
	if g.Node(node) == nil {
		return fmt.Errorf("%w: unknown graph node", engine.ErrNotFound)
	}
	receipt, err := s.Engine.PublishReplicaBranch(r.Context(), g, node, replica, req, identityFrom(r.Context()).Name, "")
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, receipt)
	return nil
}
