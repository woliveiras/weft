#!/usr/bin/env sh
# Builds temporary test-owned images on the dedicated sandbox daemon only.
set -eu
container=${WEFT_TEST_SANDBOX_CONTAINER:-weft-sandbox-daemon-1}
service=$(docker inspect --format '{{index .Config.Labels "com.docker.compose.service"}}' "$container")
[ "$service" = sandbox-daemon ] || { echo 'Expected dedicated sandbox-daemon' >&2; exit 1; }
task_tmp=$(mktemp -d)
image="weft-egress-test:$(date +%s)-$$"
proxy_image="$image-proxy"
image_built=false
proxy_built=false
cleanup() {
 status=$?
 trap - EXIT HUP INT TERM
 if [ "$image_built" = true ]; then docker exec "$container" docker image rm "$image" >/dev/null || status=1; fi
 if [ "$proxy_built" = true ]; then docker exec "$container" docker image rm "$proxy_image" >/dev/null || status=1; fi
 rm -f "$task_tmp/docker-sandbox" "$task_tmp/weft-egress" "$task_tmp/probe" "$task_tmp/probe.go" "$task_tmp/Dockerfile"
 rmdir "$task_tmp" || status=1
 exit "$status"
}
trap cleanup EXIT HUP INT TERM
cat > "$task_tmp/docker-sandbox" <<'WRAPPER'
#!/usr/bin/env sh
exec docker exec -i "$WEFT_TEST_SANDBOX_CONTAINER" docker "$@"
WRAPPER
chmod 700 "$task_tmp/docker-sandbox"
export WEFT_TEST_SANDBOX_CONTAINER="$container"
export WEFT_TEST_ENV_DOCKER_BIN="$task_tmp/docker-sandbox"
export WEFT_TEST_EGRESS_IMAGE="$image"
export WEFT_TEST_EGRESS_PROXY_IMAGE="$proxy_image"
arch=$(docker exec "$container" docker version --format '{{.Server.Arch}}')
cd "$(dirname "$0")/../server"
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$task_tmp/weft-egress" ./cmd/weft-egress
cat > "$task_tmp/probe.go" <<'PROBE'
package main
import("fmt";"net/http";"os";"time")
func main(){
 t:=&http.Transport{Proxy:http.ProxyFromEnvironment}
 if len(os.Args)>2 && os.Args[2]=="direct" {t.Proxy=nil}
 c:=&http.Client{Transport:t,Timeout:15*time.Second}
 r,e:=c.Get(os.Args[1]);if e!=nil{fmt.Println(e);os.Exit(1)};defer r.Body.Close();fmt.Println(r.StatusCode)
 if r.StatusCode<200||r.StatusCode>=400{os.Exit(2)}
}
PROBE
CGO_ENABLED=0 GOOS=linux GOARCH="$arch" go build -o "$task_tmp/probe" "$task_tmp/probe.go"
cat > "$task_tmp/Dockerfile" <<'BUILD'
FROM alpine:3.22
COPY weft-egress /weft-egress
COPY probe /probe
USER 10001:10001
ENTRYPOINT ["/weft-egress"]
BUILD
tar -C "$task_tmp" -cf - Dockerfile weft-egress probe | "$task_tmp/docker-sandbox" build --network none -t "$image" -
image_built=true
cat > "$task_tmp/Dockerfile" <<'PROXY'
FROM scratch
COPY weft-egress /weft-egress
USER 10001:10001
ENTRYPOINT ["/weft-egress"]
PROXY
tar -C "$task_tmp" -cf - Dockerfile weft-egress probe | "$task_tmp/docker-sandbox" build --network none -t "$proxy_image" -
proxy_built=true
go test ./internal/app -run '^TestEgressRealSandbox$' -count=1 -v -timeout 4m

go test ./internal/environment -run '^TestPreparationEgressRealSandbox$' -count=1 -v -timeout 6m
