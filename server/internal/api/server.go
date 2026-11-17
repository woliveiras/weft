// Package api is the HTTP surface of the control plane: the public REST API
// (/api/v1), the live event stream (SSE), the runtime callback API used by
// external agents (/internal/v1), health and metrics, and the web console.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"runtime/debug"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/config"
	"github.com/woliveiras/weft/server/internal/cost"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/engine"
	"github.com/woliveiras/weft/server/internal/environment"
	"github.com/woliveiras/weft/server/internal/events"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/hitl"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/llmconfig"
	"github.com/woliveiras/weft/server/internal/mcpconfig"
	"github.com/woliveiras/weft/server/internal/observability"
	"github.com/woliveiras/weft/server/internal/queue"
	"github.com/woliveiras/weft/server/internal/repositoryimport"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/scm"
	"github.com/woliveiras/weft/server/internal/secrets"
	"github.com/woliveiras/weft/server/internal/storage"
	"github.com/woliveiras/weft/server/internal/taskrun"
	"github.com/woliveiras/weft/server/internal/tools"
	"github.com/woliveiras/weft/server/internal/trigger"
	"github.com/woliveiras/weft/server/internal/workspace"
)

// Version is set at build time (-ldflags "-X .../api.Version=...").
var Version = "dev"

type Deps struct {
	TriggerManager    *trigger.Manager
	RepositoryImports *repositoryimport.Manager
	MCP               *mcpconfig.Manager
	SCM               scm.Manager
	Reviews           *review.Manager
	PreviewGateway    *review.Gateway
	// Gateway runs server-side tool calls (issue triggers) under the same
	// authorization and audit as agents. Optional.
	Gateway    *tools.Gateway
	Config     *config.Config
	Engine     *engine.Engine
	Store      storage.Store
	Broker     queue.Broker
	Bus        *events.Bus
	Approvals  *hitl.Service
	Models     *llm.Registry
	Providers  *llmconfig.Manager
	Tools      *tools.Registry
	Workspaces *workspace.Manager
	// Environments is optional; without it the environment routes answer 404.
	Environments *environment.Manager
	Proposals    *environment.Proposals
	Auth         *Authenticator
	Log          *slog.Logger
	Metrics      http.Handler
}

type Server struct {
	Deps
	mux      *http.ServeMux
	internal *http.ServeMux // callback API; == mux unless server.internalAddr is set
}

func New(d Deps) *Server {
	s := &Server{Deps: d, mux: http.NewServeMux()}
	s.internal = s.mux
	if d.Config.Server.InternalAddr != "" {
		s.internal = http.NewServeMux()
	}
	s.routes()
	return s
}

// Handler serves the public API, console, health and metrics (and the
// callback API too, unless it has a listener of its own).
func (s *Server) Handler() http.Handler { return s.middleware(s.mux) }

// InternalHandler serves the callback API when server.internalAddr is set.
func (s *Server) InternalHandler() http.Handler {
	if s.internal == s.mux {
		return nil
	}
	return s.middleware(s.internal)
}

type route struct {
	pattern string
	role    config.Role // "" = public
	h       func(http.ResponseWriter, *http.Request) error
}

func (s *Server) routes() {
	R := config.RoleViewer
	O := config.RoleOperator
	A := config.RoleAdmin
	routes := []route{
		{"GET /api/v1/tasks", R, s.listTasks},
		{"POST /api/v1/tasks", O, s.saveTask},
		{"POST /api/v1/tasks/prompt", O, s.resolveTaskPrompt},
		{"POST /api/v1/tasks/submit", A, s.submitTaskPrompt},
		{"GET /api/v1/tasks/{tid}", R, s.getTask},
		{"PUT /api/v1/tasks/{tid}", O, s.saveTask},
		{"GET /api/v1/tasks/{tid}/authorization", R, s.taskAuthorization},
		{"PUT /api/v1/tasks/{tid}/authorization", A, s.taskAuthorization},
		{"GET /api/v1/tasks/{tid}/review", R, s.reviewTask},
		{"GET /api/v1/tasks/{tid}/progress", R, s.taskProgress},
		{"POST /api/v1/tasks/{tid}/{action}", A, s.taskAction},
		{"GET /api/v1/tasks/{tid}/cleanup", A, s.taskCleanup},
		{"POST /api/v1/tasks/{tid}/cleanup", A, s.taskCleanup},
		{"GET /api/v1/delegation-policy", R, s.delegationPolicy},
		{"PUT /api/v1/delegation-policy", A, s.delegationPolicy},
		{"GET /api/v1/delegation-profiles", R, s.delegationProfiles},
		{"POST /api/v1/delegation-profiles", A, s.delegationProfiles},
		{"GET /api/v1/delegation-profiles/{profile}", R, s.delegationProfiles},
		{"PUT /api/v1/delegation-profiles/{profile}", A, s.delegationProfiles},
		{"GET /api/v1/mcp/connections", R, s.mcpList},
		{"GET /api/v1/mcp/templates", R, s.mcpTemplates},
		{"GET /api/v1/mcp/history", R, s.mcpHistory},
		{"POST /api/v1/mcp/connections/{connection}/oauth/start", A, s.mcpOAuthStart},
		{"POST /api/v1/mcp/connections/{connection}/oauth/revoke", A, s.mcpOAuthRevoke},
		{"GET /api/v1/mcp/oauth/callback", "", s.mcpOAuthCallback},
		{"POST /api/v1/mcp/oauth/complete", A, s.mcpOAuthComplete},
		{"GET /api/v1/mcp/oauth/config", R, s.mcpOAuthConfig},
		{"GET /api/v1/mcp/connections/{connection}/oauth/preview", A, s.mcpOAuthPreview},
		{"GET /api/v1/mcp/oauth/client/{connection}", "", s.mcpOAuthClientDocument},
		{"GET /api/v1/mcp/tools", R, s.mcpTools},
		{"POST /api/v1/mcp/connections", A, s.mcpPut},
		{"GET /api/v1/mcp/connections/{connection}", R, s.mcpGet},
		{"PUT /api/v1/mcp/connections/{connection}", A, s.mcpPut},
		{"DELETE /api/v1/mcp/connections/{connection}", A, s.mcpDelete},
		{"POST /api/v1/mcp/connections/{connection}/test", O, s.mcpRefresh},
		{"POST /api/v1/mcp/connections/{connection}/tools/refresh", O, s.mcpRefresh},
		{"GET /api/v1/reviews", O, s.listReviews},
		{"POST /api/v1/reviews/{review}/{action}", O, s.reviewAction},
		{"GET /healthz", "", s.healthz},
		{"GET /readyz", "", s.readyz},
		{"GET /api/v1/info", R, s.info},
		{"GET /api/v1/graphs", R, s.listGraphs},
		{"POST /api/v1/graphs", A, s.createGraph},
		{"POST /api/v1/graphs/validate", R, s.validateGraph},
		{"GET /api/v1/graphs/{id}", R, s.getGraph},
		{"PUT /api/v1/graphs/{id}", A, s.updateGraph},
		{"DELETE /api/v1/graphs/{id}", A, s.deleteGraph},
		{"GET /api/v1/graphs/{id}/versions", R, s.graphVersions},
		{"GET /api/v1/graphs/{id}/export", R, s.exportGraph},
		{"POST /api/v1/graphs/{id}/{action}", O, s.graphAction},
		{"GET /api/v1/graphs/{id}/status", R, s.graphStatus},
		{"POST /api/v1/environments/cleanup", A, s.cleanupEnvironments},
		{"GET /api/v1/graphs/{id}/environment", R, s.getEnvironment},
		{"GET /api/v1/graphs/{id}/environment/proxies", R, s.environmentProxies},
		{"GET /api/v1/graphs/{id}/environment/blocked-history", R, s.environmentBlockedHistory},
		{"GET /api/v1/graphs/{id}/environment/discovery", R, s.discoverEnvironment},
		{"POST /api/v1/graphs/{id}/environment/resolve", R, s.resolveEnvironment},
		{"GET /api/v1/graphs/{id}/environment/proposal", R, s.getEnvironmentProposal},
		{"POST /api/v1/graphs/{id}/environment/proposal", A, s.proposeEnvironment},
		{"POST /api/v1/graphs/{id}/environment/proposal/cancel", A, s.cancelEnvironmentProposal},
		{"POST /api/v1/graphs/{id}/environment/proposal/retry", A, s.retryEnvironmentProposal},
		{"POST /api/v1/graphs/{id}/environment/build", O, s.buildEnvironment},
		{"POST /api/v1/graphs/{id}/environment/reset", A, s.resetEnvironment},
		{"POST /api/v1/graphs/{id}/nodes/{node}/{action}", O, s.nodeAction},
		{"PUT /api/v1/graphs/{id}/nodes/{node}/scale", O, s.scaleNode},
		{"GET /api/v1/graphs/{id}/nodes/{node}/chat", R, s.listChat},
		{"POST /api/v1/graphs/{id}/nodes/{node}/chat", O, s.postChat},
		{"POST /api/v1/graphs/{id}/channels/{channel}/messages", O, s.inject},
		{"GET /api/v1/graphs/{id}/messages", R, s.listMessages},
		{"GET /api/v1/messages/{mid}", R, s.getMessage},
		{"GET /api/v1/graphs/{id}/executions", R, s.listExecutions},
		{"GET /api/v1/executions/{eid}", R, s.getExecution},
		{"GET /api/v1/executions/{eid}/tree", R, s.executionTree},
		{"POST /api/v1/executions/{eid}/cancel", O, s.cancelExecution},
		{"GET /api/v1/graphs/{id}/events", R, s.listEvents},
		{"GET /api/v1/events/stream", R, s.stream},
		{"GET /api/v1/activity", R, s.activityView},
		{"GET /api/v1/activity/health", R, s.activityHealth},
		{"GET /api/v1/approvals", R, s.listApprovals},
		{"POST /api/v1/approvals/{aid}/{decision}", O, s.decide},
		{"GET /api/v1/blocked-history-retention", R, s.historyRetention},
		{"PUT /api/v1/blocked-history-retention", A, s.saveHistoryRetention},
		{"GET /api/v1/network-policy", R, s.networkPolicy},
		{"POST /api/v1/network-policy/preview", A, s.previewNetworkPolicy},
		{"PUT /api/v1/network-policy", A, s.saveNetworkPolicy},
		{"GET /api/v1/skills", R, s.skills},
		{"POST /api/v1/skills", A, s.saveSkill},
		{"GET /api/v1/skills/{sid}", R, s.skill},
		{"PUT /api/v1/skills/{sid}", A, s.saveSkill},
		{"DELETE /api/v1/skills/{sid}", A, s.saveSkill},
		{"GET /api/v1/graphs/{id}/instructions", R, s.inspectInstructions},
		{"GET /api/v1/executions/{eid}/instructions", R, s.executionInstructions},
		{"GET /api/v1/providers", R, s.providers},
		{"POST /api/v1/providers", A, s.createProvider},
		{"POST /api/v1/providers/test", A, s.testProviderDraft},
		{"GET /api/v1/providers/{name}", R, s.getProvider},
		{"PUT /api/v1/providers/{name}", A, s.updateProvider},
		{"DELETE /api/v1/providers/{name}", A, s.deleteProvider},
		{"POST /api/v1/providers/{name}/test", O, s.testProvider},
		{"GET /api/v1/providers/{name}/models", R, s.models},
		{"GET /api/v1/provider-plugins", R, s.providerPlugins},
		{"PUT /api/v1/provider-plugins/{id}", A, s.updateProviderPlugin},
		{"GET /api/v1/provider-settings", R, s.providerSettings},
		{"GET /api/v1/trigger-policy", R, s.triggerPolicy},
		{"GET /api/v1/triggers", R, s.listTriggers},
		{"POST /api/v1/triggers", A, s.createTrigger},
		{"GET /api/v1/triggers/{trigger}", R, s.getTrigger},
		{"PUT /api/v1/triggers/{trigger}", A, s.updateTrigger},
		{"GET /api/v1/triggers/{trigger}/matches", O, s.previewTrigger},
		{"DELETE /api/v1/triggers/{trigger}", A, s.deleteTrigger},
		{"POST /api/v1/triggers/{trigger}/resolve-hold", O, s.resolveTriggerHold},
		{"POST /api/v1/triggers/{trigger}/pause", A, s.pauseTrigger},
		{"POST /api/v1/triggers/{trigger}/resume", A, s.resumeTrigger},
		{"GET /api/v1/scm/connections", R, s.scmConnections},
		{"POST /api/v1/scm/connections", A, s.createSCMConnection},
		{"PUT /api/v1/scm/connections/{name}", A, s.updateSCMConnection},
		{"DELETE /api/v1/scm/connections/{name}", A, s.deleteSCMConnection},
		{"POST /api/v1/scm/connections/{name}/test", O, s.testSCMConnection},
		{"GET /api/v1/cost/prices", R, s.getPrices},
		{"PUT /api/v1/cost/prices", A, s.putPrices},
		{"GET /api/v1/cost/policy", R, s.getCostPolicy},
		{"PUT /api/v1/cost/policy", A, s.putCostPolicy},
		{"GET /api/v1/cost/budgets", R, s.getBudgetDefinitions},
		{"GET /api/v1/cost/budgets/preview", R, s.previewBudgets},
		{"GET /api/v1/cost/ledgers", R, s.getLedgers},
		{"GET /api/v1/cost/spend", R, s.getSpend},
		{"PUT /api/v1/cost/budgets", A, s.putBudgetDefinitions},
		{"GET /api/v1/cost/budgets/{scope}/{budget}", R, s.getBudget},
		{"POST /api/v1/cost/budgets/{scope}/{budget}/raise", A, s.raiseBudget},
		{"PUT /api/v1/provider-settings", A, s.updateProviderSettings},
		{"GET /api/v1/tools", R, s.toolsList},
		{"GET /api/v1/graphs/{id}/git/{view}", R, s.inspectGit},
		{"GET /api/v1/graphs/{id}/checkpoints", R, s.listCheckpoints},
		{"POST /api/v1/graphs/{id}/checkpoints/restore", A, s.restoreCheckpoint},
		{"GET /api/v1/graphs/{id}/files", R, s.listFiles},
		{"GET /api/v1/graphs/{id}/files/content", R, s.fileContent},
	}
	for _, rt := range routes {
		s.mux.Handle(rt.pattern, s.wrap(rt))
	}
	metrics := s.Metrics
	if metrics == nil {
		metrics = promhttp.Handler()
	}
	if s.Config.Server.ProtectMetrics {
		s.mux.Handle("GET /metrics", s.wrap(route{"GET /metrics", config.RoleViewer, func(w http.ResponseWriter, r *http.Request) error {
			metrics.ServeHTTP(w, r)
			return nil
		}}))
	} else {
		s.mux.Handle("GET /metrics", metrics)
	}
	s.projectRoutes()
	s.internalRoutes()
	if dir := s.Config.Server.WebDir; dir != "" {
		s.mux.Handle("GET /", spa(dir))
	}
}

// wrap applies authentication, authorisation and error mapping.
func (s *Server) wrap(rt route) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rt.role != "" {
			id, err := s.Auth.Authenticate(r)
			if err != nil {
				writeErr(w, http.StatusUnauthorized, "unauthorized", err.Error(), nil)
				return
			}
			if !id.Role.Allows(rt.role) {
				writeErr(w, http.StatusForbidden, "forbidden", fmt.Sprintf("role %s required", rt.role), nil)
				return
			}
			r = r.WithContext(context.WithValue(r.Context(), ctxKey{}, id))
		}
		if r.Method != http.MethodGet && strings.HasPrefix(r.PathValue("id"), taskrun.GraphPrefix) {
			writeErr(w, 409, "conflict", "Task execution copies are immutable; use task controls.", nil)
			return
		}
		if err := rt.h(w, r); err != nil {
			s.writeError(w, r, err)
		}
	})
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, err error) {
	var ve *engine.ValidationError
	var pve *llmconfig.ValidationError
	var cve *cost.ValidationError
	var sve *scm.ValidationError
	var tfe *trigger.FieldError
	switch {
	case errors.As(err, &tfe):
		writeErr(w, http.StatusUnprocessableEntity, "invalid", err.Error(), map[string]any{"valid": false, "errors": tfe.Errors})
	case errors.As(err, &sve):
		writeErr(w, http.StatusUnprocessableEntity, "invalid", err.Error(), map[string]any{"valid": false, "errors": sve.Errors})
	case errors.As(err, &cve):
		writeErr(w, http.StatusUnprocessableEntity, "invalid", err.Error(), map[string]any{"valid": false, "errors": cve.Errors})
	case errors.As(err, &ve):
		writeErr(w, http.StatusUnprocessableEntity, "invalid", err.Error(), ve.Report)
	case errors.As(err, &pve):
		writeErr(w, http.StatusUnprocessableEntity, "invalid", err.Error(), map[string]any{"valid": false, "errors": pve.Errors})
	case errors.Is(err, mcpconfig.ErrOAuth):
		writeErr(w, http.StatusBadGateway, "oauth", err.Error(), nil)
	case errors.Is(err, mcpconfig.ErrInvalid):
		writeErr(w, http.StatusUnprocessableEntity, "invalid", err.Error(), nil)
	case errors.Is(err, secrets.ErrNoKey):
		writeErr(w, http.StatusConflict, "secret_key_missing", err.Error(), nil)
	case errors.Is(err, engine.ErrNotFound), errors.Is(err, storage.ErrNotFound):
		writeErr(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, engine.ErrConflict), errors.Is(err, hitl.ErrNotPending), errors.Is(err, storage.ErrConflict):
		writeErr(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, engine.ErrInvalid), errors.Is(err, errBadRequest):
		writeErr(w, http.StatusBadRequest, "invalid", err.Error(), nil)
	case errors.Is(err, tools.ErrDenied), errors.Is(err, tools.ErrRejected):
		writeErr(w, http.StatusForbidden, "forbidden", err.Error(), nil)
	case errors.Is(err, environment.ErrBuildRunning):
		writeErr(w, http.StatusConflict, "conflict", err.Error(), nil)
	case errors.Is(err, environment.ErrNotDefined), errors.Is(err, environment.ErrInvalidDefinition):
		writeErr(w, http.StatusBadRequest, "invalid", err.Error(), nil)
	case errors.Is(err, tools.ErrUnknownTool):
		writeErr(w, http.StatusNotFound, "not_found", err.Error(), nil)
	case errors.Is(err, tools.ErrRateLimited):
		writeErr(w, http.StatusTooManyRequests, "rate_limited", err.Error(), nil)
	case errors.Is(err, context.Canceled):
		// client went away
	default:
		s.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeErr(w, http.StatusInternalServerError, "internal", "internal error", nil)
	}
}

var errBadRequest = errors.New("bad request")

func badRequest(format string, a ...any) error {
	return fmt.Errorf("%w: %s", errBadRequest, fmt.Sprintf(format, a...))
}

func writeErr(w http.ResponseWriter, code int, kind, msg string, details any) {
	body := map[string]any{"error": msg, "code": kind}
	if details != nil {
		body["details"] = details
	}
	writeJSON(w, code, body)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// middleware: panic recovery, tracing, metrics, security headers, CORS,
// body limits, access logging.
func (s *Server) middleware(next http.Handler) http.Handler {
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rw := &statusWriter{ResponseWriter: w, code: 200}
		defer func() {
			if p := recover(); p != nil {
				s.Log.Error("panic", "panic", p, "stack", string(debug.Stack()))
				writeErr(rw, http.StatusInternalServerError, "internal", "internal error", nil)
			}
		}()
		h := rw.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if origin := r.Header.Get("Origin"); origin != "" && s.allowedOrigin(origin) {
			h.Set("Access-Control-Allow-Origin", origin)
			h.Set("Vary", "Origin")
			h.Set("Access-Control-Allow-Headers", "Authorization, Content-Type, If-Match")
			h.Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			if r.Method == http.MethodOptions {
				rw.WriteHeader(http.StatusNoContent)
				return
			}
		}
		if r.Body != nil {
			r.Body = http.MaxBytesReader(rw, r.Body, s.Config.Server.MaxBodyBytes)
		}
		ctx := prop.Extract(r.Context(), propagation.HeaderCarrier(r.Header))
		route := r.Method + " " + r.URL.Path
		if _, pat := s.mux.Handler(r); pat != "" {
			route = pat
		}
		var span trace.Span
		if !strings.HasPrefix(r.URL.Path, "/api/v1/events/stream") && !strings.HasPrefix(r.URL.Path, "/internal/") && r.URL.Path != "/metrics" && r.URL.Path != "/healthz" {
			ctx, span = observability.Start(ctx, route, attribute.String("http.method", r.Method), attribute.String("http.route", route))
			defer span.End()
		}
		next.ServeHTTP(rw, r.WithContext(ctx))
		if span != nil {
			span.SetAttributes(attribute.Int("http.status_code", rw.code))
		}
		if strings.HasPrefix(route, "GET /api/v1/events/stream") {
			return
		}
		d := time.Since(start)
		observability.APIRequests.WithLabelValues(r.Method, route, strconv.Itoa(rw.code)).Inc()
		observability.APIDuration.WithLabelValues(r.Method, route).Observe(d.Seconds())
		if r.URL.Path != "/healthz" && r.URL.Path != "/metrics" && (strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/internal/")) {
			s.Log.Debug("http", "method", r.Method, "path", r.URL.Path, "status", rw.code, "ms", d.Milliseconds())
		}
	})
}

func (s *Server) allowedOrigin(o string) bool {
	for _, a := range s.Config.Server.CORSOrigins {
		if a == "*" || a == o {
			return true
		}
	}
	return false
}

type statusWriter struct {
	http.ResponseWriter
	code    int
	written bool
}

func (w *statusWriter) WriteHeader(c int) {
	if !w.written {
		w.code = c
		w.written = true
	}
	w.ResponseWriter.WriteHeader(c)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// --- health ---------------------------------------------------------------

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, map[string]string{"status": "ok"})
	return nil
}

func (s *Server) readyz(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Second)
	defer cancel()
	checks := map[string]string{"storage": "ok", "queue": "ok", "sandbox": "not configured"}
	code := 200
	if err := s.Store.Ping(ctx); err != nil {
		checks["storage"], code = "unavailable", 503
	}
	if err := s.Broker.Ping(ctx); err != nil {
		checks["queue"], code = "unavailable", 503
	}
	// The sandbox daemon only matters to docker-isolated agents, so it is
	// reported but does not make the platform unready.
	if d := s.Config.Isolation.Docker; d.Host != "" {
		if _, err := tools.PingDocker(ctx, d.Bin, d.Host); err != nil {
			checks["sandbox"] = "unavailable"
		} else {
			checks["sandbox"] = "ok"
		}
	}
	writeJSON(w, code, checks)
	return nil
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) error {
	rts := []string{}
	for k := range s.Engine.KnownRuntimes() {
		rts = append(rts, string(k))
	}
	writeJSON(w, 200, map[string]any{
		"version": Version, "instance": s.Config.Instance,
		"queue": s.Broker.Name(), "storage": s.Store.Name(),
		"providers": s.Models.Names(), "defaultProvider": s.Models.DefaultName(),
		"runtimes": rts, "grafanaUrl": s.Config.Observability.GrafanaURL,
		"identity":      identityFrom(r.Context()),
		"nativeProfile": s.Config.NativeProfile,
	})
	return nil
}

// --- graphs ---------------------------------------------------------------

type graphSummary struct {
	Graph  *domain.Graph       `json:"graph"`
	Record *domain.GraphRecord `json:"record"`
}

func (s *Server) listGraphs(w http.ResponseWriter, r *http.Request) error {
	gs, err := s.Store.ListGraphs(r.Context())
	if err != nil {
		return err
	}
	out := []graphSummary{}
	for _, g := range gs {
		rec, err := s.Store.GetGraphRecord(r.Context(), g.Metadata.ID)
		if err != nil {
			rec = &domain.GraphRecord{ID: g.Metadata.ID, State: domain.GraphStopped, DesiredState: domain.GraphStopped}
		}
		out = append(out, graphSummary{g, rec})
	}
	writeJSON(w, 200, out)
	return nil
}

// decodeGraph accepts JSON or YAML (by Content-Type, or sniffed). Keys the
// graph schema does not define are returned as issues rather than dropped.
func decodeGraph(r *http.Request) (*domain.Graph, []graph.Issue, error) {
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, nil, badRequest("read body: %v", err)
	}
	ct := r.Header.Get("Content-Type")
	trim := strings.TrimSpace(string(b))
	isYAML := strings.Contains(ct, "yaml") || (!strings.HasPrefix(trim, "{") && trim != "")
	g, unknown, err := graph.Decode(b, isYAML)
	if err != nil {
		if isYAML {
			return nil, nil, badRequest("invalid YAML: %v", err)
		}
		return nil, nil, badRequest("invalid JSON: %v", err)
	}
	return g, unknown, nil
}

// unknownFieldsError reports undefined keys like any other validation
// finding (422 with field paths), so editors can point at them.
func unknownFieldsError(unknown []graph.Issue) error {
	return &engine.ValidationError{Report: &graph.Report{Valid: false, Errors: unknown, Warnings: []graph.Issue{}}}
}

func (s *Server) createGraph(w http.ResponseWriter, r *http.Request) error {
	g, unknown, err := decodeGraph(r)
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return unknownFieldsError(unknown)
	}
	// Derive the id the way normalisation does, without touching the
	// document (it is stored as written).
	id := g.Metadata.ID
	if id == "" {
		id = graph.Slug(g.Metadata.Name)
	}
	if strings.HasPrefix(id, taskrun.GraphPrefix) {
		return badRequest("task-run- is reserved for task execution copies")
	}
	if _, err := s.Store.GetGraph(r.Context(), id, 0); err == nil {
		return fmt.Errorf("%w: graph %q already exists (use PUT to update)", engine.ErrConflict, id)
	}
	if err := s.validateEnvironmentOrigin(r.Context(), g); err != nil {
		return err
	}
	g, rec, err := s.Engine.SaveGraph(r.Context(), g, 0, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, graphSummary{g, rec})
	return nil
}

func (s *Server) validateGraph(w http.ResponseWriter, r *http.Request) error {
	g, unknown, err := decodeGraph(r)
	if err != nil {
		return err
	}
	rep := s.Engine.Validate(g)
	if s.MCP != nil {
		if err := s.MCP.ValidateGrants(r.Context(), g); err != nil {
			if !errors.Is(err, mcpconfig.ErrInvalid) {
				return err
			}
			rep.Valid = false
			rep.Errors = append(rep.Errors, graph.Issue{Path: "spec.nodes", Message: err.Error()})
		}
	}
	if len(unknown) > 0 {
		rep.Errors = append(unknown, rep.Errors...)
		rep.Valid = false
	}
	writeJSON(w, 200, rep)
	return nil
}

func (s *Server) getGraph(w http.ResponseWriter, r *http.Request) error {
	v, _ := strconv.Atoi(r.URL.Query().Get("version"))
	// The document as written: editors must not save defaults back into nodes.
	g, rec, err := s.Engine.GetGraphDocument(r.Context(), r.PathValue("id"), v)
	if err != nil {
		return err
	}
	writeJSON(w, 200, graphSummary{g, rec})
	return nil
}

func (s *Server) updateGraph(w http.ResponseWriter, r *http.Request) error {
	g, unknown, err := decodeGraph(r)
	if err != nil {
		return err
	}
	if len(unknown) > 0 {
		return unknownFieldsError(unknown)
	}
	id := r.PathValue("id")
	if g.Metadata.ID == "" {
		g.Metadata.ID = id
	}
	if g.Metadata.ID != id {
		return badRequest("metadata.id %q does not match the URL", g.Metadata.ID)
	}
	if _, err := s.Store.GetGraph(r.Context(), id, 0); err != nil {
		return engine.ErrNotFound
	}
	expected := 0
	if im := strings.Trim(r.Header.Get("If-Match"), `" W/`); im != "" {
		expected, err = strconv.Atoi(im)
		if err != nil {
			return badRequest("If-Match must be a version number")
		}
	}
	if err := s.validateEnvironmentOrigin(r.Context(), g); err != nil {
		return err
	}
	g, rec, err := s.Engine.SaveGraph(r.Context(), g, expected, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, 200, graphSummary{g, rec})
	return nil
}

func (s *Server) deleteGraph(w http.ResponseWriter, r *http.Request) error {
	id := r.PathValue("id")
	actor := identityFrom(r.Context()).Name
	hadEnv := false
	if s.Environments != nil {
		hadEnv, _ = s.Environments.Exists(r.Context(), id)
	}
	if err := s.Engine.DeleteGraph(r.Context(), id, actor); err != nil {
		return err
	}
	// The graph is gone; its environment images and caches go too. Failures
	// leave orphans in the sandbox daemon only, so they are logged.
	if hadEnv {
		if err := s.Environments.Reset(r.Context(), id, actor); err != nil {
			s.Log.Warn("environment cleanup after graph deletion", "graph", id, "err", err)
		}
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *Server) graphVersions(w http.ResponseWriter, r *http.Request) error {
	vs, err := s.Store.ListGraphVersions(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, vs)
	return nil
}

func (s *Server) exportGraph(w http.ResponseWriter, r *http.Request) error {
	v, _ := strconv.Atoi(r.URL.Query().Get("version"))
	g, _, err := s.Engine.GetGraphDocument(r.Context(), r.PathValue("id"), v)
	if err != nil {
		return err
	}
	if r.URL.Query().Get("format") == "json" {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.json"`, g.Metadata.ID))
		writeJSON(w, 200, g)
		return nil
	}
	b, err := yaml.Marshal(g)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", "application/yaml")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s.yaml"`, g.Metadata.ID))
	_, err = w.Write(b)
	return err
}

func (s *Server) graphAction(w http.ResponseWriter, r *http.Request) error {
	id, action := r.PathValue("id"), r.PathValue("action")
	switch action {
	case "start", "stop", "pause", "resume", "kill":
	default:
		return engine.ErrNotFound
	}
	if s.Proposals != nil && (action == "stop" || action == "kill" || action == "pause") {
		if err := s.Proposals.DisableResume(r.Context(), id); err != nil {
			return err
		}
	}
	if err := s.Engine.GraphAction(r.Context(), id, action, identityFrom(r.Context()).Name); err != nil {
		return err
	}
	return s.graphStatus(w, r)
}

func (s *Server) graphStatus(w http.ResponseWriter, r *http.Request) error {
	st, err := s.Engine.Status(r.Context(), r.PathValue("id"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, st)
	return nil
}

func (s *Server) nodeAction(w http.ResponseWriter, r *http.Request) error {
	action := r.PathValue("action")
	switch action {
	case "start", "stop", "pause", "resume", "kill":
	default:
		return engine.ErrNotFound
	}
	if err := s.Engine.NodeAction(r.Context(), r.PathValue("id"), r.PathValue("node"), action, identityFrom(r.Context()).Name); err != nil {
		return err
	}
	return s.graphStatus(w, r)
}

func (s *Server) scaleNode(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Replicas int `json:"replicas"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return badRequest("invalid body: %v", err)
	}
	if err := s.Engine.Scale(r.Context(), r.PathValue("id"), r.PathValue("node"), body.Replicas, identityFrom(r.Context()).Name); err != nil {
		return err
	}
	return s.graphStatus(w, r)
}

func (s *Server) listChat(w http.ResponseWriter, r *http.Request) error {
	ms, err := s.Store.ListOperatorMessages(r.Context(), r.PathValue("id"), r.PathValue("node"), 200)
	if err != nil {
		return err
	}
	if ms == nil {
		ms = []*domain.OperatorMessage{}
	}
	writeJSON(w, 200, ms)
	return nil
}

func (s *Server) postChat(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Text    string `json:"text"`
		Replica *int   `json:"replica"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return badRequest("invalid body: %v", err)
	}
	rep := -1
	if body.Replica != nil {
		rep = *body.Replica
	}
	m, err := s.Engine.SendOperatorMessage(r.Context(), r.PathValue("id"), r.PathValue("node"), rep, body.Text, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, m)
	return nil
}

func (s *Server) inject(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Type     string          `json:"type"`
		Payload  json.RawMessage `json:"payload"`
		Target   string          `json:"target"`
		Metadata map[string]any  `json:"metadata"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		return badRequest("invalid body: %v", err)
	}
	m, err := s.Engine.Inject(r.Context(), r.PathValue("id"), r.PathValue("channel"), body.Type, body.Payload, body.Target, body.Metadata, identityFrom(r.Context()).Name)
	if err != nil {
		return err
	}
	writeJSON(w, http.StatusCreated, m)
	return nil
}

func qint(r *http.Request, k string, def int) int {
	if v, err := strconv.Atoi(r.URL.Query().Get(k)); err == nil {
		return v
	}
	return def
}

func (s *Server) listMessages(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	ms, err := s.Store.ListMessages(r.Context(), storage.MessageFilter{
		GraphID: r.PathValue("id"), Channel: q.Get("channel"), NodeID: q.Get("node"),
		CorrelationID: q.Get("correlationId"), Status: domain.MessageStatus(q.Get("status")), Limit: qint(r, "limit", 100),
	})
	if err != nil {
		return err
	}
	if ms == nil {
		ms = []*domain.MessageRecord{}
	}
	writeJSON(w, 200, ms)
	return nil
}

func (s *Server) getMessage(w http.ResponseWriter, r *http.Request) error {
	ms, err := s.Store.GetMessage(r.Context(), r.PathValue("mid"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, ms)
	return nil
}

func (s *Server) listExecutions(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	xs, err := s.Store.ListExecutions(r.Context(), storage.ExecutionFilter{
		GraphID: r.PathValue("id"), NodeID: q.Get("node"), Status: domain.ExecutionStatus(q.Get("status")), Limit: qint(r, "limit", 50),
	})
	if err != nil {
		return err
	}
	if xs == nil {
		xs = []*domain.Execution{}
	}
	writeJSON(w, 200, xs)
	return nil
}

func (s *Server) getExecution(w http.ResponseWriter, r *http.Request) error {
	x, err := s.Store.GetExecution(r.Context(), r.PathValue("eid"))
	if err != nil {
		return err
	}
	writeJSON(w, 200, x)
	return nil
}

func eventFilter(r *http.Request, graphID string) storage.EventFilter {
	q := r.URL.Query()
	f := storage.EventFilter{GraphID: graphID, NodeID: q.Get("node"), Limit: qint(r, "limit", 200), Ascending: true}
	f.AfterSeq, _ = strconv.ParseInt(q.Get("after"), 10, 64)
	if t := q.Get("types"); t != "" {
		for _, x := range strings.Split(t, ",") {
			f.Types = append(f.Types, domain.EventType(strings.TrimSpace(x)))
		}
	}
	return f
}

func (s *Server) listEvents(w http.ResponseWriter, r *http.Request) error {
	f := eventFilter(r, r.PathValue("id"))
	if r.URL.Query().Get("after") == "" {
		// Without a resume point, return the latest page (still ascending).
		f.Ascending = false
		evs, err := s.Store.ListEvents(r.Context(), f)
		if err != nil {
			return err
		}
		for i, j := 0, len(evs)-1; i < j; i, j = i+1, j-1 {
			evs[i], evs[j] = evs[j], evs[i]
		}
		if evs == nil {
			evs = []*domain.Event{}
		}
		writeJSON(w, 200, evs)
		return nil
	}
	evs, err := s.Store.ListEvents(r.Context(), f)
	if err != nil {
		return err
	}
	if evs == nil {
		evs = []*domain.Event{}
	}
	writeJSON(w, 200, evs)
	return nil
}

// stream is the SSE endpoint. Clients resume with ?after=<seq> or the
// standard Last-Event-ID header; without either they start at "now".
func (s *Server) stream(w http.ResponseWriter, r *http.Request) error {
	fl, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming unsupported")
	}
	graphID := r.URL.Query().Get("graph")
	f := eventFilter(r, graphID)
	f.Limit = 500
	if lei := r.Header.Get("Last-Event-ID"); lei != "" {
		f.AfterSeq, _ = strconv.ParseInt(lei, 10, 64)
	} else if r.URL.Query().Get("after") == "" {
		f.AfterSeq = s.Bus.LatestSeq(r.Context(), graphID)
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	_, _ = fmt.Fprintf(w, "retry: 3000\n: connected\n\n")
	fl.Flush()

	ctx := r.Context()
	err := s.Bus.Subscribe(ctx, f, func(ev *domain.Event) error {
		if ev == nil {
			// Heartbeat: a comment keeps proxies from closing an idle stream.
			if _, err := fmt.Fprint(w, ": ping\n\n"); err != nil {
				return err
			}
			fl.Flush()
			return nil
		}
		b, err := json.Marshal(ev)
		if err != nil {
			return nil
		}
		if _, err := fmt.Fprintf(w, "id: %d\nevent: %s\ndata: %s\n\n", ev.Seq, ev.Type, b); err != nil {
			return err
		}
		fl.Flush()
		return nil
	})
	if errors.Is(err, context.Canceled) {
		return nil
	}
	return err
}

func (s *Server) listApprovals(w http.ResponseWriter, r *http.Request) error {
	q := r.URL.Query()
	as, err := s.Store.ListApprovals(r.Context(), storage.ApprovalFilter{
		GraphID: q.Get("graph"), Status: domain.ApprovalStatus(q.Get("status")), Limit: qint(r, "limit", 200),
	})
	if err != nil {
		return err
	}
	if as == nil {
		as = []*domain.Approval{}
	}
	writeJSON(w, 200, as)
	return nil
}

func (s *Server) decide(w http.ResponseWriter, r *http.Request) error {
	var body struct {
		Comment string `json:"comment"`
		Answer  string `json:"answer"`
	}
	if r.ContentLength != 0 {
		_ = json.NewDecoder(r.Body).Decode(&body)
	}
	approve := false
	switch r.PathValue("decision") {
	case "approve":
		approve = true
	case "reject":
	default:
		return engine.ErrNotFound
	}
	pending, err := s.Store.GetApproval(r.Context(), r.PathValue("aid"))
	if err != nil {
		return err
	}
	if pending.Kind == domain.ApprovalEnvironment {
		if !identityFrom(r.Context()).Role.Allows(config.RoleAdmin) {
			return fmt.Errorf("%w: environment approval requires admin", tools.ErrDenied)
		}
		if s.Proposals == nil {
			return engine.ErrNotFound
		}
		if approve {
			if err := s.Proposals.ValidateDecision(r.Context(), pending); err != nil {
				return badRequest("%v", err)
			}
		}
	}
	a, err := s.Approvals.Decide(r.Context(), r.PathValue("aid"), approve, identityFrom(r.Context()).Name, body.Comment, body.Answer)
	if err != nil {
		return err
	}
	writeJSON(w, 200, a)
	return nil
}

func (s *Server) models(w http.ResponseWriter, r *http.Request) error {
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	ms, err := s.Models.Models(ctx, r.PathValue("name"))
	if err != nil {
		return fmt.Errorf("%w: %v", engine.ErrNotFound, err)
	}
	if ms == nil {
		ms = []llm.ModelInfo{}
	}
	writeJSON(w, 200, ms)
	return nil
}

func (s *Server) toolsList(w http.ResponseWriter, r *http.Request) error {
	writeJSON(w, 200, s.Tools.Describe())
	return nil
}

// --- workspace browser ----------------------------------------------------

func (s *Server) browseRoot(r *http.Request) (string, error) {
	g, _, err := s.Engine.GetGraph(r.Context(), r.PathValue("id"), 0)
	if err != nil {
		return "", err
	}
	root, err := s.Workspaces.GraphDir(g)
	if err != nil {
		return "", err
	}
	if node := r.URL.Query().Get("node"); node != "" {
		n := g.Node(node)
		if n == nil || n.Workspace.Mode != domain.WorkspaceWorktree {
			return "", badRequest("unknown worktree node")
		}
		rep := 0
		if value := r.URL.Query().Get("replica"); value != "" {
			var parseErr error
			rep, parseErr = strconv.Atoi(value)
			if parseErr != nil || rep < 0 {
				return "", badRequest("invalid replica")
			}
		}
		root = filepath.Join(s.Workspaces.Root, ".worktrees", g.Metadata.ID, fmt.Sprintf("%s-%d", node, rep))
	} else if r.URL.Query().Get("replica") != "" {
		return "", badRequest("replica requires node")
	}
	rel, relativeErr := filepath.Rel(s.Workspaces.Root, root)
	if relativeErr == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		current := s.Workspaces.Root
		for _, part := range strings.Split(rel, string(filepath.Separator)) {
			current = filepath.Join(current, part)
			info, err := os.Lstat(current)
			if err == nil && info.Mode()&os.ModeSymlink != 0 {
				return "", badRequest("workspace symlinks are not supported")
			}
		}
	} else if info, err := os.Lstat(root); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return "", badRequest("workspace symlinks are not supported")
	}

	return root, nil
}

func (s *Server) listFiles(w http.ResponseWriter, r *http.Request) error {
	root, err := s.browseRoot(r)
	if err != nil {
		return err
	}
	_, rel, err := browsePath(root, r.URL.Query().Get("path"))
	if err != nil {
		return badRequest("%v", err)
	}
	jail, err := os.OpenRoot(root)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, 200, []any{})
		return nil
	}
	if err != nil {
		return badRequest("workspace unavailable")
	}
	defer jail.Close()
	directory, err := jail.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, 200, []any{})
		return nil
	}
	if err != nil {
		return badRequest("directory unavailable")
	}
	defer directory.Close()
	entries, err := directory.ReadDir(2000)
	if errors.Is(err, io.EOF) {
		err = nil
	}
	if errors.Is(err, os.ErrNotExist) {
		writeJSON(w, 200, []any{})
		return nil
	}
	if err != nil {
		return badRequest("%v", err)
	}
	type file struct {
		Name    string    `json:"name"`
		Path    string    `json:"path"`
		Dir     bool      `json:"dir"`
		Size    int64     `json:"size"`
		ModTime time.Time `json:"modTime"`
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	out := []file{}
	for _, e := range entries {
		if strings.EqualFold(e.Name(), ".git") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		out = append(out, file{e.Name(), filepath.ToSlash(filepath.Join(rel, e.Name())), e.IsDir(), info.Size(), info.ModTime()})
		if len(out) >= 2000 {
			break
		}
	}
	writeJSON(w, 200, out)
	return nil
}

func (s *Server) fileContent(w http.ResponseWriter, r *http.Request) error {
	root, err := s.browseRoot(r)
	if err != nil {
		return err
	}
	_, rel, err := browsePath(root, r.URL.Query().Get("path"))
	if err != nil {
		return badRequest("%v", err)
	}
	jail, err := os.OpenRoot(root)
	if err != nil {
		return fmt.Errorf("%w: workspace unavailable", engine.ErrNotFound)
	}
	defer jail.Close()
	info, err := jail.Stat(rel)
	if err != nil {
		return fmt.Errorf("%w: file unavailable", engine.ErrNotFound)
	}
	if !info.Mode().IsRegular() {
		return badRequest("only regular files can be read")
	}
	f, err := jail.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return badRequest("file unavailable")
	}
	defer f.Close()
	openedInfo, err := f.Stat()
	if err != nil {
		return badRequest("file unavailable")
	}
	if !openedInfo.Mode().IsRegular() {
		return badRequest("only regular files can be read")
	}
	// Served as plain text with nosniff so agent-written HTML can never
	// execute in the console's origin.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'")
	_, err = io.Copy(w, io.LimitReader(f, 4<<20))
	return err
}

// spa serves the built console with an index.html fallback.
func spa(dir string) http.Handler {
	fs := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Join(dir, filepath.Clean("/"+r.URL.Path))
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			if r.URL.Path != "/" && !strings.HasSuffix(r.URL.Path, "/") {
				r.URL.Path = "/"
			}
		}
		if r.URL.Path == "/" || strings.HasSuffix(r.URL.Path, ".html") {
			w.Header().Set("Cache-Control", "no-cache")
		} else {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		}
		fs.ServeHTTP(w, r)
	})
}
