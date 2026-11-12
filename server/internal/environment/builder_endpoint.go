package environment

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func builderEndpointName(graphID string) string { return "weft-builder-endpoint-" + graphID }

// Buildx cannot store the default Docker endpoint when its TLS material comes
// from the environment. Give the worker a named copy of that endpoint.
func (m *Manager) builderContext(ctx context.Context, graphID string) (string, error) {
	host := m.opt.DockerHost
	if host == "" {
		host = os.Getenv("DOCKER_HOST")
	}
	if !strings.HasPrefix(host, "tcp://") || (os.Getenv("DOCKER_TLS_VERIFY") == "" && os.Getenv("DOCKER_TLS") == "") {
		return "", nil
	}
	name := builderEndpointName(graphID)
	// The graph build lease covers this name. A previous process may have
	// stopped before cleanup, so refresh the context from current credentials.
	var contexts bytes.Buffer
	if err := m.docker(ctx, nil, &contexts, "context", "ls", "--format", "{{.Name}}"); err != nil {
		return "", builderConnectionError(ctx, "list Docker contexts")
	}
	for _, existing := range strings.Fields(contexts.String()) {
		if existing == name {
			if err := m.removeBuilderContext(ctx, name); err != nil {
				return "", err
			}
			break
		}
	}
	var args []string
	if dir := os.Getenv("DOCKER_CERT_PATH"); dir != "" {
		// Explicit client files prevent Docker from silently dropping a missing
		// certificate or key in an explicitly configured credential directory.
		args = append(args, "--tlscert", filepath.Join(dir, "cert.pem"), "--tlskey", filepath.Join(dir, "key.pem"))
	}
	args = append(args, "context", "create", "--from", "default", name)
	if err := m.docker(ctx, nil, io.Discard, args...); err != nil {
		return "", builderConnectionError(ctx, "create Docker TLS context (check the Docker client certificates and config directory)")
	}
	return name, nil
}

func (m *Manager) removeBuilderContext(ctx context.Context, name string) error {
	if err := m.docker(ctx, nil, io.Discard, "context", "rm", name); err != nil {
		return builderConnectionError(ctx, "remove Docker TLS context")
	}
	return nil
}

func builderConnectionError(ctx context.Context, operation string) error {
	// Docker's diagnostics can include credential paths. Keep them out of the
	// persisted build log and of the error exposed through the environment API.
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("sandbox daemon build connection: %s: %w", operation, err)
	}
	return fmt.Errorf("sandbox daemon build connection: could not %s", operation)
}
