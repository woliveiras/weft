# Weft control plane: weftd (API + engine + console) and the weft CLI.
#   docker build -t weft/weftd .
# Multi-stage: the console is built with Node, the binaries with Go, and the
# runtime image carries only what agents' tools need (git, python3, a shell).

FROM node:22-alpine AS web
RUN corepack enable
WORKDIR /web
COPY web/package.json web/pnpm-lock.yaml ./
RUN pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm run build

FROM golang:1.26-alpine AS build
WORKDIR /src/server
# -p=2 bounds compiler memory so the image builds on small Docker VMs (2 GB).
ENV CGO_ENABLED=0 GOFLAGS="-trimpath -p=2"
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
ARG VERSION=dev
RUN go build -ldflags "-s -w -X github.com/woliveiras/weft/server/internal/api.Version=${VERSION}" -o /out/weftd ./cmd/weftd \
 && go build -ldflags "-s -w" -o /out/weft ./cmd/weft \
 && go build -ldflags "-s -w" -o /out/weft-preview-linux ./cmd/weft-preview

FROM alpine:3.22
# git: workspaces and worktrees; python3: QA agents run test suites through
# shell.exec in the process sandbox; docker-cli: docker isolation, talking to
# the dedicated sandbox daemon (WEFT_SANDBOX_DOCKER_HOST); tini: signal
# handling for children.
RUN apk add --no-cache ca-certificates git python3 tini docker-cli docker-cli-buildx tzdata \
 && addgroup -S -g 10001 weft && adduser -S -u 10001 -G weft -h /var/lib/weft weft \
 && mkdir -p /var/lib/weft/data /var/lib/weft/workspaces /app \
 && chown -R weft:weft /var/lib/weft
COPY --from=build /out/weftd /out/weft /usr/local/bin/
COPY --from=build /out/weft-preview-linux /usr/local/lib/weft/weft-preview-linux
COPY --from=web /web/dist /app/web
COPY examples /app/examples
COPY deploy/config/weft.yaml /etc/weft/weft.yaml
ENV WEFT_WEB_DIR=/app/web \
    WEFT_PREVIEW_RELAY=/usr/local/lib/weft/weft-preview-linux \
    WEFT_STORAGE_DIR=/var/lib/weft/data \
    WEFT_WORKSPACES=/var/lib/weft/workspaces \
    WEFT_QUEUE_DIR=/var/lib/weft/data/queues \
    HOME=/var/lib/weft
USER 10001
WORKDIR /var/lib/weft
EXPOSE 8080
HEALTHCHECK --interval=10s --timeout=3s --start-period=10s --retries=6 \
  CMD wget -qO- http://127.0.0.1:8080/healthz >/dev/null || exit 1
ENTRYPOINT ["/sbin/tini", "--", "weftd"]
CMD ["--config", "/etc/weft/weft.yaml"]
