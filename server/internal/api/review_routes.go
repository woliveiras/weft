package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/woliveiras/weft/server/internal/reviewroute"
)

func (s *Server) reviewRouteManager() (*reviewroute.Manager, error) {
	if s.ReviewRoutes == nil {
		return nil, reviewroute.ErrUnavailable
	}
	return s.ReviewRoutes, nil
}
func decodeReviewRoute(r *http.Request, preview bool) (reviewroute.Input, error) {
	var body struct {
		Route   reviewroute.Input `json:"route"`
		Confirm bool              `json:"confirm"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 8193))
	if err != nil || len(raw) > 8192 {
		return body.Route, badRequest("invalid or oversized review route")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var extra any
	if dec.Decode(&body) != nil || dec.Decode(&extra) != io.EOF || (!preview && !body.Confirm) {
		return body.Route, badRequest("review the route and confirm its activation boundary")
	}
	return body.Route, nil
}
func (s *Server) listReviewRoutes(w http.ResponseWriter, r *http.Request) error {
	m, err := s.reviewRouteManager()
	if err != nil {
		return err
	}
	rows, next, err := m.ListPage(r.Context(), r.URL.Query().Get("after"), 100)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"items": rows, "next": next})
	return nil
}
func (s *Server) getReviewRoute(w http.ResponseWriter, r *http.Request) error {
	m, err := s.reviewRouteManager()
	if err != nil {
		return err
	}
	out, err := m.Get(r.Context(), r.PathValue("name"))
	if err != nil {
		return err
	}
	w.Header().Set("ETag", strconv.Quote(strconv.FormatInt(out.Revision, 10)))
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) previewReviewRoute(w http.ResponseWriter, r *http.Request) error {
	m, err := s.reviewRouteManager()
	if err != nil {
		return err
	}
	in, err := decodeReviewRoute(r, true)
	if err != nil {
		return err
	}
	out, err := m.Preview(r.Context(), in)
	if err != nil {
		return err
	}
	writeJSON(w, 200, out)
	return nil
}
func (s *Server) saveReviewRoute(w http.ResponseWriter, r *http.Request) error {
	m, err := s.reviewRouteManager()
	if err != nil {
		return err
	}
	in, err := decodeReviewRoute(r, false)
	if err != nil {
		return err
	}
	var revision int64
	if r.Method == http.MethodPut {
		revision, err = issueConnectionRevision(r)
		if err != nil {
			return err
		}
		if in.Name != r.PathValue("name") {
			return badRequest("route name must match its URL")
		}
	}
	out, err := m.Put(r.Context(), in, revision, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	code := 201
	if revision > 0 {
		code = 200
	}
	writeJSON(w, code, out)
	return nil
}
func (s *Server) getReviewRouteReceipt(w http.ResponseWriter, r *http.Request) error {
	m, err := s.reviewRouteManager()
	if err != nil {
		return err
	}
	out, err := m.GetReceipt(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, out)
	return nil
}

// Keep transport errors generic; scope/CAS failures are actionable conflicts.
func reviewRouteError(err error) bool { return errors.Is(err, reviewroute.ErrInvalid) }

func (s *Server) listReviewRouteReceipts(w http.ResponseWriter, r *http.Request) error {
	m, err := s.reviewRouteManager()
	if err != nil {
		return err
	}
	rows, next, err := m.ReceiptsPage(r.Context(), r.URL.Query().Get("after"), 100)
	if err != nil {
		return err
	}
	writeJSON(w, 200, map[string]any{"items": rows, "next": next})
	return nil
}

func (s *Server) reconcileReviewRouteReceipt(w http.ResponseWriter, r *http.Request) error {
	return s.recoverReviewRouteReceipt(w, r, false)
}
func (s *Server) retryReviewRouteReceipt(w http.ResponseWriter, r *http.Request) error {
	return s.recoverReviewRouteReceipt(w, r, true)
}
func (s *Server) recoverReviewRouteReceipt(w http.ResponseWriter, r *http.Request, retry bool) error {
	m, err := s.reviewRouteManager()
	if err != nil {
		return err
	}
	rev, err := issueConnectionRevision(r)
	if err != nil {
		return err
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 129))
	if err != nil || len(raw) > 128 {
		return badRequest("invalid recovery confirmation")
	}
	var body struct {
		Confirm bool `json:"confirm"`
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var extra any
	if dec.Decode(&body) != nil || dec.Decode(&extra) != io.EOF || !body.Confirm {
		return badRequest("confirm the reviewed route recovery")
	}
	var out reviewroute.Receipt
	if retry {
		out, err = m.RetryFailed(r.Context(), r.PathValue("id"), rev, identityFrom(r.Context()).Name)
	} else {
		out, err = m.Reconcile(r.Context(), r.PathValue("id"), rev, identityFrom(r.Context()).Name)
	}
	if err != nil {
		return err
	}
	writeJSON(w, 200, out)
	return nil
}
