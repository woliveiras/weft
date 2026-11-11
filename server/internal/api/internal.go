package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/runtime"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/tools"
)

// internalRoutes is the runtime callback API (Runtime Protocol v1). Each
// request authenticates with the per-execution token handed to the agent
// in TurnInput.callback; the token authorises exactly what the turn may do
// and stops working when the turn ends. Deployments should keep /internal
// off public ingress (it is only meant for agent runtimes).
func (s *Server) internalRoutes() {
	base := "/internal/v1/executions/{eid}"
	s.internal.Handle("GET "+base+"/tools", s.cb(s.cbListTools))
	s.internal.Handle("POST "+base+"/tools/{tool}", s.cb(s.cbInvoke))
	s.internal.Handle("POST "+base+"/llm/chat", s.cb(s.cbChat))
	s.internal.Handle("POST "+base+"/emit", s.cb(s.cbEmit))
	s.internal.Handle("GET "+base+"/state/{key...}", s.cb(s.cbStateGet))
	s.internal.Handle("PUT "+base+"/state/{key...}", s.cb(s.cbStatePut))
	s.internal.Handle("POST "+base+"/output", s.cb(s.cbOutput))
	s.internal.Handle("POST "+base+"/ask", s.cb(s.cbAsk))
}

type cbHandler func(w http.ResponseWriter, r *http.Request, env runtime.TurnEnv, p *tools.Principal) error

func (s *Server) cb(h cbHandler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		env, p, err := s.Engine.Callback(r.PathValue("eid"), tok)
		if err != nil {
			writeErr(w, http.StatusUnauthorized, "unauthorized", err.Error(), nil)
			return
		}
		if sp, ok := env.(interface{ SpanContext() trace.SpanContext }); ok && sp.SpanContext().IsValid() {
			ctx := trace.ContextWithRemoteSpanContext(r.Context(), sp.SpanContext())
			ctx, span := observability.Start(ctx, "callback "+r.Method+" "+strings.TrimPrefix(r.URL.Path, "/internal/v1/executions/"+r.PathValue("eid")))
			defer span.End()
			r = r.WithContext(ctx)
		}
		if err := h(w, r, env, p); err != nil {
			s.writeError(w, r, err)
		}
	})
}

func (s *Server) cbListTools(w http.ResponseWriter, r *http.Request, _ runtime.TurnEnv, p *tools.Principal) error {
	out := s.Engine.ToolSchemas(p)
	if out == nil {
		out = []llm.ToolSchema{}
	}
	writeJSON(w, 200, out)
	return nil
}

func (s *Server) cbInvoke(w http.ResponseWriter, r *http.Request, env runtime.TurnEnv, _ *tools.Principal) error {
	args, err := io.ReadAll(r.Body)
	if err != nil {
		return badRequest("read body: %v", err)
	}
	if len(strings.TrimSpace(string(args))) == 0 {
		args = []byte(`{}`)
	}
	res, err := env.InvokeTool(r.Context(), r.PathValue("tool"), args)
	if err != nil {
		if errors.Is(err, tools.ErrDenied) || errors.Is(err, tools.ErrRejected) || errors.Is(err, tools.ErrUnknownTool) || errors.Is(err, tools.ErrRateLimited) {
			return err
		}
		// Budget exhaustion and similar are reported to the agent as tool errors.
		writeJSON(w, 200, tools.Result{Content: err.Error(), IsError: true})
		return nil
	}
	writeJSON(w, 200, res)
	return nil
}

func (s *Server) cbChat(w http.ResponseWriter, r *http.Request, env runtime.TurnEnv, _ *tools.Principal) error {
	var req llm.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return badRequest("invalid ChatRequest: %v", err)
	}
	resp, err := env.Chat(r.Context(), &req)
	if err != nil {
		writeErr(w, http.StatusBadGateway, "llm_error", err.Error(), nil)
		return nil
	}
	writeJSON(w, 200, resp)
	return nil
}

func (s *Server) cbEmit(w http.ResponseWriter, r *http.Request, env runtime.TurnEnv, _ *tools.Principal) error {
	var em runtime.Emit
	if err := json.NewDecoder(r.Body).Decode(&em); err != nil {
		return badRequest("invalid Emit: %v", err)
	}
	if err := env.Emit(r.Context(), em); err != nil {
		return badRequest("%v", err)
	}
	writeJSON(w, 200, map[string]bool{"queued": true})
	return nil
}

func stateKey(r *http.Request) (string, error) {
	// PathValue is already unescaped by the router; decoding again would
	// corrupt keys containing '%'.
	k := r.PathValue("key")
	if k == "" || len(k) > 512 {
		return "", badRequest("invalid state key")
	}
	return k, nil
}

func (s *Server) cbStateGet(w http.ResponseWriter, r *http.Request, env runtime.TurnEnv, _ *tools.Principal) error {
	k, err := stateKey(r)
	if err != nil {
		return err
	}
	v, err := env.StateGet(r.Context(), k)
	if errors.Is(err, storage.ErrNotFound) {
		return engine.ErrNotFound
	}
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	_, err = w.Write(v)
	return err
}

func (s *Server) cbStatePut(w http.ResponseWriter, r *http.Request, env runtime.TurnEnv, _ *tools.Principal) error {
	k, err := stateKey(r)
	if err != nil {
		return err
	}
	v, err := io.ReadAll(io.LimitReader(r.Body, 1<<20+1))
	if err != nil {
		return badRequest("read body: %v", err)
	}
	if err := env.StatePut(r.Context(), k, v); err != nil {
		return badRequest("%v", err)
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) cbOutput(w http.ResponseWriter, r *http.Request, env runtime.TurnEnv, _ *tools.Principal) error {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return badRequest("invalid body: %v", err)
	}
	env.Output(r.Context(), body.Text)
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) cbAsk(w http.ResponseWriter, r *http.Request, env runtime.TurnEnv, _ *tools.Principal) error {
	var body struct {
		Question string `json:"question"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || strings.TrimSpace(body.Question) == "" {
		return badRequest("question is required")
	}
	ans, err := env.AskHuman(r.Context(), body.Question)
	if err != nil {
		writeErr(w, http.StatusConflict, "no_answer", err.Error(), nil)
		return nil
	}
	writeJSON(w, 200, map[string]string{"answer": ans})
	return nil
}
