FROM golang:1.26-alpine AS build
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/internal ./internal
COPY server/cmd/weft-sandbox-exec ./cmd/weft-sandbox-exec
RUN CGO_ENABLED=0 go build -trimpath -o /out/weft-sandbox-exec ./cmd/weft-sandbox-exec

FROM golang:1.26-alpine
RUN apk add --no-cache git make ca-certificates && addgroup -g 10001 sandbox && adduser -D -u 10001 -G sandbox sandbox
COPY --from=build /out/weft-sandbox-exec /usr/local/bin/weft-sandbox-exec
USER 10001:10001
WORKDIR /workspace
CMD ["sleep", "infinity"]
