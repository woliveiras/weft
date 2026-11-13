package environment

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

func builderEndpointName(graphID string) string { return "weft-builder-endpoint-" + graphID }

func (m *Manager) usesBuilderTLSContext() bool {
	host := m.opt.DockerHost
	if host == "" {
		host = os.Getenv("DOCKER_HOST")
	}
	return strings.HasPrefix(host, "tcp://") && (os.Getenv("DOCKER_TLS_VERIFY") != "" || os.Getenv("DOCKER_TLS") != "")
}

// Buildx cannot store the default Docker endpoint when its TLS material comes
// from the environment. Give the worker a named copy of that endpoint.
func (m *Manager) builderContext(ctx context.Context, graphID string, log io.Writer) (string, error) {
	if !m.usesBuilderTLSContext() {
		return "", nil
	}
	name := builderEndpointName(graphID)
	// The graph build lease covers this name. A previous process may have
	// stopped before cleanup, so refresh the context from current credentials.
	if err := m.removeStaleBuilderContext(ctx, name); err != nil {
		return "", err
	}
	var args []string
	if dir := os.Getenv("DOCKER_CERT_PATH"); dir != "" {
		// Explicit client files prevent Docker from silently dropping a missing
		// certificate or key in an explicitly configured credential directory.
		args = append(args, "--tlscert", filepath.Join(dir, "cert.pem"), "--tlskey", filepath.Join(dir, "key.pem"))
	}
	args = append(args, "context", "create", "--from", "default", name)
	if err := m.builderDocker(ctx, true, io.Discard, "create Docker TLS context", args...); err != nil {
		// Docker can write the context metadata before a TLS store write fails.
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), builderCleanupTimeout)
		defer cancel()
		m.warnBuilderCleanup(graphID, log, m.removeStaleBuilderContext(cleanup, name))
		return "", err
	}
	return name, nil
}

func (m *Manager) removeStaleBuilderContext(ctx context.Context, name string) error {
	var contexts bytes.Buffer
	if err := m.builderDocker(ctx, true, &contexts, "list Docker contexts", "context", "ls", "--format", "{{.Name}}"); err != nil {
		return err
	}
	for _, existing := range strings.Fields(contexts.String()) {
		if existing == name {
			return m.removeBuilderContext(ctx, name)
		}
	}
	return nil
}

func (m *Manager) removeBuilderContext(ctx context.Context, name string) error {
	return m.builderDocker(ctx, true, io.Discard, "remove Docker TLS context", "context", "rm", name)
}

type builderConnectionError struct {
	operation, diagnostic string
}

func (e *builderConnectionError) Error() string {
	return "sandbox daemon build connection: could not " + e.operation
}

// Capture TLS connection commands so raw CLI output never enters the build log.
// On failure, retain the sanitized diagnostics for the server log boundary.
func (m *Manager) builderDocker(ctx context.Context, tls bool, out io.Writer, operation string, args ...string) error {
	if !tls {
		return m.docker(ctx, nil, out, args...)
	}
	var output bytes.Buffer
	if err := m.docker(ctx, nil, &output, args...); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		diagnostic := strings.TrimSpace(output.String())
		if output.Len() > 8192 {
			diagnostic = "Docker diagnostic exceeded the capture limit"
		} else if diagnostic == "" {
			diagnostic = err.Error()
		}
		return &builderConnectionError{operation, sanitizeBuilderDiagnostic(diagnostic, os.Getenv("DOCKER_CERT_PATH"))}
	}
	_, err := fmt.Fprint(out, sanitizeBuilderDiagnostic(output.String(), os.Getenv("DOCKER_CERT_PATH")))
	return err
}

var (
	tlsFileFlag = regexp.MustCompile(`--tls(?:cert|key|cacert)(?:=|[ \t]+)(?:"[^"\n]*"|'[^'\n]*'|[^\s]+)`)
	pemPath     = regexp.MustCompile(`(?i)"[^"\n]*\.pem"|'[^'\n]*\.pem'|(?:[/~]|\.{1,2}/)[^\n"'<>:=]*?\.pem|[^\s"'<>:=]+\.pem`)
)

func sanitizeBuilderDiagnostic(output, certPath string) string {
	if strings.Contains(output, "-----BEGIN ") || strings.Contains(output, "-----END ") {
		return "[Docker output contained PEM material]"
	}
	if certPath != "" && certPath != "/" {
		output = strings.ReplaceAll(output, certPath, "[TLS directory]")
	}
	output = tlsFileFlag.ReplaceAllString(output, "[TLS option]")
	return pemPath.ReplaceAllString(output, "[PEM file]")
}

func (m *Manager) warnBuilderCleanup(graphID string, log io.Writer, err error) {
	if err == nil {
		return
	}
	fmt.Fprintf(log, "==> Warning: %v\n", err)
	attrs := []any{"graph", graphID, "err", err}
	var connection *builderConnectionError
	if errors.As(err, &connection) {
		attrs = append(attrs, "operation", connection.operation, "diagnostic", connection.diagnostic)
	}
	m.opt.Log.Warn("environment builder cleanup incomplete", attrs...)
}
