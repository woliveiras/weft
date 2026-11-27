package protocol

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
)

type Agent func(context.Context, *TurnInput, *Client) (TurnResult, error)

func NewHandler(controllerOrigins []string, secret string, agents map[string]Agent) (http.Handler, error) {
	origins := map[string]bool{}
	for _, origin := range controllerOrigins {
		origin = strings.TrimSuffix(strings.TrimSpace(origin), "/")
		u, err := url.Parse(origin)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || origin != u.Scheme+"://"+u.Host {
			return nil, errors.New("invalid controller origin")
		}
		origins[origin] = true
	}
	if len(origins) == 0 || len(agents) == 0 {
		return nil, errors.New("controller origins and agents required")
	}
	handlers := map[string]Agent{}
	var names []string
	for name, agent := range agents {
		if !identifier.MatchString(name) || agent == nil {
			return nil, errors.New("invalid agent registration")
		}
		handlers[name] = agent
		names = append(names, name)
	}
	slices.Sort(names)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"status": "ok", "agents": names, "framework": "eino"})
	})
	mux.HandleFunc("POST /v1/turn", func(w http.ResponseWriter, r *http.Request) {
		if secret != "" && subtle.ConstantTimeCompare([]byte(secret), []byte(r.Header.Get("X-Weft-Runtime-Secret"))) != 1 {
			writeError(w, 401, "invalid runtime secret")
			return
		}
		var in TurnInput
		body := http.MaxBytesReader(w, r.Body, 32<<20)
		decoder := json.NewDecoder(body)
		if err := decoder.Decode(&in); err != nil {
			var tooLarge *http.MaxBytesError
			if errors.As(err, &tooLarge) {
				writeError(w, 413, "turn too large")
			} else {
				writeError(w, 400, "invalid turn")
			}
			return
		}
		if decoder.Decode(new(any)) != io.EOF {
			writeError(w, 400, "invalid turn")
			return
		}
		name := in.Agent
		if name == "" {
			name = in.NodeID
		}
		agent := handlers[name]
		if agent == nil {
			writeError(w, 404, "unknown agent")
			return
		}
		client, err := bind(&in, origins)
		if err != nil {
			writeError(w, 400, "invalid callback")
			return
		}
		defer client.closed.Store(true)
		ctx := propagation.TraceContext{}.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		ctx, span := otel.Tracer("weft/eino").Start(ctx, "agent.turn")
		defer span.End()
		span.SetAttributes(attribute.String("agent", name))
		start := time.Now()
		result, err := agent(ctx, &in, client)
		outcome := "ok"
		defer func() {
			span.SetAttributes(attribute.String("outcome", outcome))
			slog.InfoContext(ctx, "agent turn", "agent", name, "outcome", outcome, "duration_ms", time.Since(start).Milliseconds())
		}()
		switch {
		case client.yielded.Load():
			outcome = "yield"
			writeJSON(w, 200, TurnResult{})
		case err != nil || ctx.Err() != nil:
			outcome = "failed"
			writeError(w, 500, "agent turn failed")
		default:
			writeJSON(w, 200, result)
		}
	})
	return mux, nil
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]string{"error": message})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	raw, err := json.Marshal(value)
	if err != nil {
		http.Error(w, "invalid agent response", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(raw)
}
