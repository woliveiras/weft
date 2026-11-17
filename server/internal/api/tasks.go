package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/tools"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/taskauth"
	"github.com/woliveiras/weft/server/internal/taskconfig"
	"github.com/woliveiras/weft/server/internal/taskrun"
)

func taskError(err error) error {
	if errors.Is(err, taskconfig.ErrInvalid) || errors.Is(err, taskauth.ErrInvalid) {
		return &engine.ValidationError{Report: &graph.Report{Errors: []graph.Issue{{Path: "task", Message: err.Error()}}}}
	}
	return err
}
func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) error {
	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil {
			return badRequest("invalid limit")
		}
		limit = n
	}
	page, err := (taskconfig.Manager{Store: s.Store}).List(r.Context(), r.URL.Query().Get("after"), limit)
	if err != nil {
		return taskError(err)
	}
	for _, d := range page.Items {
		if d.Status != "draft" {
			v, e := s.taskRunner().View(r.Context(), d.ID)
			if e != nil {
				return e
			}
			d.ObservedState = v.State
		}
	}
	writeJSON(w, 200, page)
	return nil
}
func (s *Server) getTask(w http.ResponseWriter, r *http.Request) error {
	d, err := (taskconfig.Manager{Store: s.Store}).Get(r.Context(), r.PathValue("tid"))
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(d.Revision, 10)))
	writeJSON(w, 200, d)
	return nil
}
func (s *Server) saveTask(w http.ResponseWriter, r *http.Request) error {
	var in taskconfig.Input
	r.Body = http.MaxBytesReader(w, r.Body, 48*1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid task JSON")
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return badRequest("expected one JSON object")
	}
	revision := int64(0)
	if r.Method == http.MethodPut {
		var err error
		revision, err = ifMatchRevision(r)
		if err != nil {
			return err
		}
		if revision < 1 {
			return badRequest("If-Match is required")
		}
	}
	d, err := (taskconfig.Manager{Store: s.Store}).Save(r.Context(), r.PathValue("tid"), in, revision, identityFrom(r.Context()).Name)
	if err != nil {
		return taskError(err)
	}
	status := 200
	if r.Method == http.MethodPost {
		status = 201
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(d.Revision, 10)))
	writeJSON(w, status, d)
	return nil
}

// TaskRunner is the task run manager the API uses (issue triggers start
// tasks with it too).
func (s *Server) TaskRunner() taskrun.Manager { return s.taskRunner() }

func (s *Server) taskRunner() taskrun.Manager {
	return taskrun.Manager{Store: s.Store, Engine: s.Engine, Workspaces: s.Workspaces, Environments: s.Environments, NativeProfile: s.Config.NativeProfile, CheckSandbox: func(ctx context.Context) error {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		cmd := tools.DockerCommand(ctx, s.Config.Isolation.Docker.Bin, s.Config.Isolation.Docker.Host, "info", "--format", "{{.ServerVersion}}")
		cmd.Stdout = io.Discard
		cmd.Stderr = io.Discard
		return cmd.Run()
	}}
}
func (s *Server) reviewTask(w http.ResponseWriter, r *http.Request) error {
	v, err := s.taskRunner().Review(r.Context(), r.PathValue("tid"))
	if err != nil {
		return taskError(err)
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) taskProgress(w http.ResponseWriter, r *http.Request) error {
	v, err := s.taskRunner().View(r.Context(), r.PathValue("tid"))
	if err != nil {
		return taskError(err)
	}
	writeJSON(w, 200, v)
	return nil
}
func (s *Server) taskAction(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Confirm bool `json:"confirm"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || !in.Confirm {
		return badRequest("explicit confirmation is required")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return badRequest("expected one JSON object")
	}
	var d *taskconfig.Draft
	var err error
	if r.PathValue("action") == "start" || r.PathValue("action") == "prepare-environment" {
		rev, e := ifMatchRevision(r)
		if e != nil {
			return e
		}
		if rev < 1 {
			return badRequest("If-Match is required")
		}
		if r.PathValue("action") == "prepare-environment" {
			d, err = s.taskRunner().PrepareEnvironment(r.Context(), r.PathValue("tid"), rev, identityFrom(r.Context()).Name)
		} else {
			d, err = s.taskRunner().Start(r.Context(), r.PathValue("tid"), rev, identityFrom(r.Context()).Name)
		}
	} else if r.PathValue("action") == "cancel" {
		d, err = s.taskRunner().Cancel(r.Context(), r.PathValue("tid"), identityFrom(r.Context()).Name)
	} else {
		return badRequest("unknown task action")
	}
	if err != nil {
		return taskError(err)
	}
	writeJSON(w, 202, d)
	return nil
}

func (s *Server) taskCleanup(w http.ResponseWriter, r *http.Request) error {
	node := r.URL.Query().Get("node")
	replica := 0
	if raw := r.URL.Query().Get("replica"); raw != "" {
		var err error
		replica, err = strconv.Atoi(raw)
		if err != nil || replica < 0 {
			return badRequest("invalid replica")
		}
	}
	if r.Method == http.MethodGet {
		preview, err := s.taskRunner().PreviewCleanup(r.Context(), r.PathValue("tid"), node, replica)
		if err != nil {
			return err
		}
		writeJSON(w, 200, preview)
		return nil
	}
	var in struct {
		Confirm     bool   `json:"confirm"`
		Fingerprint string `json:"fingerprint"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil || !in.Confirm || len(in.Fingerprint) != 64 {
		return badRequest("review and explicit cleanup confirmation are required")
	}
	var extra any
	if dec.Decode(&extra) != io.EOF {
		return badRequest("expected one JSON object")
	}
	if err := s.taskRunner().Cleanup(r.Context(), r.PathValue("tid"), node, replica, in.Fingerprint, identityFrom(r.Context()).Name); err != nil {
		return err
	}
	writeJSON(w, 200, map[string]bool{"removed": true})
	return nil
}

// Persist first so model failures and disconnected clients retain the request.
func (s *Server) resolveTaskPrompt(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 24<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in taskconfig.PromptInput
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid prompt request")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return badRequest("expected one JSON object")
	}
	m := taskconfig.Manager{Store: s.Store}
	d, err := m.SavePrompt(r.Context(), in, identityFrom(r.Context()).Name)
	if err != nil {
		return taskError(err)
	}
	d, err = m.ResolvePrompt(r.Context(), d, s.Models, identityFrom(r.Context()).Name)
	if err != nil {
		return taskError(err)
	}
	writeJSON(w, http.StatusOK, d)
	return nil
}

// Submission is administrator-only like task start. The prompt is not authority:
// the explicit confirm flag and existing review/start checks remain mandatory.
func (s *Server) submitTaskPrompt(w http.ResponseWriter, r *http.Request) error {
	r.Body = http.MaxBytesReader(w, r.Body, 24<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	var in struct {
		taskconfig.PromptInput
		Key     string `json:"key"`
		Confirm bool   `json:"confirm"`
	}
	if err := dec.Decode(&in); err != nil || !in.Confirm {
		return badRequest("confirm prompt submission with existing workflow permissions")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return badRequest("expected one JSON object")
	}
	actor := identityFrom(r.Context()).Name
	m := taskconfig.Manager{Store: s.Store}
	d, fresh, err := m.SaveSubmission(r.Context(), in.PromptInput, in.Key, actor)
	if err != nil {
		return taskError(err)
	}
	if fresh {
		d, err = m.ResolvePrompt(r.Context(), d, s.Models, actor)
		if err != nil {
			return taskError(err)
		}
	}
	var review *taskrun.Review
	if d.Status == "draft" {
		review, err = s.taskRunner().Review(r.Context(), d.ID)
		if err != nil {
			return taskError(err)
		}
		if fresh && review.CanStart {
			d, err = s.taskRunner().Start(r.Context(), d.ID, d.Revision, actor)
			if err != nil {
				return taskError(err)
			}
			review = nil
		}
	}
	writeJSON(w, 200, struct {
		Task   *taskconfig.Draft `json:"task"`
		Review *taskrun.Review   `json:"review,omitempty"`
	}{d, review})
	return nil
}
