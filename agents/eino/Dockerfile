FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -o /out/weft-eino-agent ./cmd/weft-eino-agent

FROM alpine:3.22
RUN apk add --no-cache ca-certificates
COPY --from=build /out/weft-eino-agent /usr/local/bin/weft-eino-agent
USER 65532:65532
ENV HOST=0.0.0.0 PORT=8000
EXPOSE 8000
HEALTHCHECK CMD wget -q -O /dev/null "http://127.0.0.1:${PORT}/v1/health" || exit 1
ENTRYPOINT ["weft-eino-agent"]
