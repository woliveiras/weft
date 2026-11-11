# The collector uses only the dedicated daemon's existing privileges.
FROM golang:1.26-alpine AS build
ENV CGO_ENABLED=0
WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN go mod download
COPY server/ ./
RUN go build -o /weft-workspace-journal ./cmd/weft-workspace-journal

FROM docker:28.5.2-dind
COPY --from=build /weft-workspace-journal /usr/local/bin/weft-workspace-journal
