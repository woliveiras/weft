package api

import (
	"encoding/json"
	"io"
	"net/http"
)

// listCheckpoints lists a graph's worktree checkpoints (NAT-07), newest first.
func (s *Server) listCheckpoints(w http.ResponseWriter, r *http.Request) error {
	list, err := s.Engine.ListCheckpoints(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, list)
	return nil
}

// restoreCheckpoint restores one replica's workspace to a checkpoint: the
// given one, or the newest recorded before an edit of node/replica. It
// rewrites workspace files, so it is an administrator action that needs an
// explicit confirmation; the engine records the state before it as a
// checkpoint and publishes an audited checkpoint.restored event.
func (s *Server) restoreCheckpoint(w http.ResponseWriter, r *http.Request) error {
	var in struct {
		Checkpoint string `json:"checkpoint"`
		Node       string `json:"node"`
		Replica    int    `json:"replica"`
		Confirm    bool   `json:"confirm"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1024)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return badRequest("invalid restore request")
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		return badRequest("expected one JSON object")
	}
	if !in.Confirm {
		return badRequest("explicit restore confirmation is required")
	}
	res, err := s.Engine.RestoreCheckpoint(r.Context(), r.PathValue("id"), in.Node, in.Replica, in.Checkpoint, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusOK, res)
	return nil
}
