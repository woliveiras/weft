package api

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
	"io"
	"net/http"
	"time"
)

func (s *Server) listReviews(w http.ResponseWriter, r *http.Request) error {
	if s.Reviews == nil {
		return storage.ErrNotFound
	}
	rs, e := s.Reviews.List(r.Context())
	if e != nil {
		return e
	}
	out := make([]map[string]any, 0, len(rs))
	for _, record := range rs {
		if !record.Allows(identityFrom(r.Context()).Name) {
			continue
		}
		v := record.View()
		v["revision"] = record.Generation
		if s.PreviewGateway != nil {
			v["url"] = s.PreviewGateway.URL(record.ID)
		}
		out = append(out, v)
	}
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) reviewAction(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()
	r = r.WithContext(ctx)
	if s.Reviews == nil {
		return badRequest("Reviews are not configured")
	}
	id := r.PathValue("review")
	action := r.PathValue("action")
	actor := identityFrom(r.Context()).Name
	existing, err := s.Reviews.Get(r.Context(), id)
	if err != nil {
		return err
	}
	if !existing.Allows(actor) {
		return tools.ErrDenied
	}
	var body struct {
		State    string `json:"state"`
		Method   string `json:"method"`
		Comment  string `json:"comment"`
		Revision int64  `json:"revision"`
		Browser  bool   `json:"browser"`
	}
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if e := d.Decode(&body); e != nil {
		return badRequest("invalid review action")
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return badRequest("unexpected trailing data")
	}
	var record *review.Record
	var e error
	switch action {
	case "decide":
		record, e = s.Reviews.Decide(r.Context(), id, body.State, body.Method, actor, body.Comment, body.Revision)
	case "revoke-access":
		record, e = s.Reviews.RevokeAccess(r.Context(), id)
	case "restart":
		record, e = s.Reviews.Restart(r.Context(), id)
	case "renew":
		record, e = s.Reviews.Renew(r.Context(), id)
	case "credential":
		var token string
		token, e = s.Reviews.Issue(r.Context(), id, actor, body.Browser)
		if e == nil {
			w.Header().Set("Cache-Control", "no-store")
			writeJSON(w, 200, map[string]string{"token": token, "url": s.PreviewGateway.URL(id)})
			return nil
		}
	case "logs":
		if existing.Session == "none" || s.Reviews.Driver == nil {
			return badRequest("no managed application logs")
		}
		record, e = s.Reviews.Get(r.Context(), id)
		if e == nil {
			var logs string
			logs, e = s.Reviews.Driver.Logs(r.Context(), record)
			if e == nil {
				writeJSON(w, 200, map[string]string{"logs": logs})
				return nil
			}
		}
	default:
		return badRequest("unknown review action")
	}
	if errors.Is(e, review.ErrForbidden) {
		return tools.ErrDenied
	}
	if errors.Is(e, review.ErrInvalid) {
		return badRequest("%v", e)
	}
	if errors.Is(e, review.ErrStale) || errors.Is(e, review.ErrUnavailable) {
		e = storage.ErrConflict
	}
	if e != nil {
		return e
	}
	v := record.View()
	v["revision"] = record.Generation
	writeJSON(w, 200, v)
	return nil
}
