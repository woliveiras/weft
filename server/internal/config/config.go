// Package config loads platform configuration from a YAML file and the
// environment. Precedence: built-in defaults < config file < environment.
// Secrets are never read from the file directly - the file names the
// environment variable or mounted file that holds them.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/environmentpolicy"
	"github.com/woliveiras/weft/server/internal/llm"
	"github.com/woliveiras/weft/server/internal/review"
	"github.com/woliveiras/weft/server/internal/sandboxcapacity"
)

type Config struct {
	// NativeProfile is set only by the native launcher, never by YAML or environment.
	NativeProfile bool          `yaml:"-" json:"-"`
	Preview       Preview       `yaml:"preview"`
	Instance      string        `yaml:"instance"`
	Server        Server        `yaml:"server"`
	Auth          Auth          `yaml:"auth"`
	Storage       Storage       `yaml:"storage"`
	Queue         Queue         `yaml:"queue"`
	Workspaces    Workspaces    `yaml:"workspaces"`
	LLM           LLM           `yaml:"llm"`
	Secrets       Secrets       `yaml:"secrets"`
	Runtimes      Runtimes      `yaml:"runtimes"`
	Isolation     Isolation     `yaml:"isolation"`
	Tools         Tools         `yaml:"tools"`
	Observability Observability `yaml:"observability"`
	Engine        Engine        `yaml:"engine"`
	// Graphs are loaded (created or updated) at startup from these files or
	// directories - GitOps-style bootstrapping of example or production graphs.
	Bootstrap Bootstrap `yaml:"bootstrap"`
}

type Preview struct {
	Addr           string   `yaml:"addr"`
	BaseURL        string   `yaml:"baseUrl"`
	ConsoleOrigins []string `yaml:"consoleOrigins"`
	RelayBinary    string   `yaml:"relayBinary"`
	TLSCert        string   `yaml:"tlsCert"`
	TLSKey         string   `yaml:"tlsKey"`
	// ContainerLocal requires the host port to be published only on loopback.
	ContainerLocal bool `yaml:"containerLocal"`
}

type Server struct {
	PublicURL string `yaml:"publicUrl"`
	Addr      string `yaml:"addr"`
	// InternalURL is how runtimes reach the callback API (http://weftd:8080).
	InternalURL string `yaml:"internalUrl"`
	// InternalAddr, when set (":8081"), serves the runtime callback API
	// (/internal/v1) on its own listener and removes it from Addr, so public
	// ingress can never reach it. InternalURL must then point at this port.
	InternalAddr string `yaml:"internalAddr"`
	// WebDir serves the built UI when set.
	WebDir      string   `yaml:"webDir"`
	CORSOrigins []string `yaml:"corsOrigins"`
	// MaxBodyBytes bounds request bodies (default 8 MiB).
	MaxBodyBytes int64 `yaml:"maxBodyBytes"`
	// ProtectMetrics requires a viewer token on /metrics (scrape with a
	// bearer token). Off by default so a stock Prometheus can scrape.
	ProtectMetrics bool `yaml:"protectMetrics"`
}

type Role string

const (
	RoleViewer   Role = "viewer"
	RoleOperator Role = "operator"
	RoleAdmin    Role = "admin"
)

func (r Role) Allows(need Role) bool {
	rank := map[Role]int{RoleViewer: 1, RoleOperator: 2, RoleAdmin: 3}
	return rank[r] >= rank[need]
}

type Token struct {
	Name      string `yaml:"name"`
	Role      Role   `yaml:"role"`
	TokenEnv  string `yaml:"tokenEnv"`
	TokenFile string `yaml:"tokenFile"`
	// Token inline is accepted only for local development; a warning is logged.
	Token string `yaml:"token"`
}

type Auth struct {
	// Disabled turns authentication off entirely (loopback-only dev use).
	Disabled bool    `yaml:"disabled"`
	Tokens   []Token `yaml:"tokens"`
	// OIDC (optional): JWTs issued by this issuer are accepted; the role is
	// taken from RoleClaim. Verification uses the issuer's JWKS.
	OIDC *OIDC `yaml:"oidc"`
}

type OIDC struct {
	Issuer    string `yaml:"issuer"`
	Audience  string `yaml:"audience"`
	RoleClaim string `yaml:"roleClaim"`
}

type Storage struct {
	Type string `yaml:"type"` // fs | sqlite | postgres
	DSN  string `yaml:"dsn"`
	Dir  string `yaml:"dir"`
}

type Queue struct {
	Type    string            `yaml:"type"` // fs | nats | redis | rabbitmq | kafka | memory
	URL     string            `yaml:"url"`
	Dir     string            `yaml:"dir"`
	Options map[string]string `yaml:"options"`
}

type Workspaces struct {
	// Root holds one directory per graph (unless the graph names a path).
	Root string `yaml:"root"`
	// HostRoot is the same directory as seen by the Docker daemon, needed to
	// bind-mount workspaces into sandbox containers when the platform itself
	// runs in a container. Empty = Root.
	HostRoot string `yaml:"hostRoot"`
	// AllowAbsolute lets graphs point at arbitrary absolute paths.
	AllowAbsolute bool `yaml:"allowAbsolute"`
}

type LLM struct {
	// DevMock offers the development "mock" plugin with one read-only "mock"
	// provider (the default until an administrator picks another). Real
	// providers are inference plugins enabled and configured in the console.
	DevMock bool `yaml:"devMock"`
}

// Static returns the providers that come from configuration (only the
// development mock) and the configured default.
func (l LLM) Static() ([]llm.ProviderConfig, string) {
	if !l.DevMock {
		return nil, ""
	}
	return []llm.ProviderConfig{{Name: "mock", Type: "mock", DefaultModel: "mock-1"}}, "mock"
}

// Secrets configures the installation key that encrypts credentials
// operators enter in the console (model provider keys, integration tokens).
type Secrets struct {
	// KeyEnv names the environment variable holding the key (base64 or hex,
	// 32 bytes). It takes precedence over KeyFile.
	KeyEnv string `yaml:"keyEnv"`
	// KeyFile holds the key when KeyEnv is unset or empty.
	KeyFile string `yaml:"keyFile"`
	// GenerateKeyFile creates KeyFile with a random key on first start when
	// no key is configured, so a fresh install can store credentials from
	// the browser. Back the file up: without it, stored credentials must be
	// entered again.
	GenerateKeyFile bool `yaml:"generateKeyFile"`
	// PreviousKeysEnv names a variable with comma-separated older keys that
	// still decrypt (key rotation); values are re-encrypted with the
	// current key at startup.
	PreviousKeysEnv string `yaml:"previousKeysEnv"`
}

type Runtimes struct {
	// External turns are bounded by the node's limits.turnTimeout.
	// SharedSecret is sent to external runtimes as X-Weft-Runtime-Secret.
	SharedSecretEnv string `yaml:"sharedSecretEnv"`
}

type Isolation struct {
	Docker DockerIsolation `yaml:"docker"`
}

type DockerIsolation struct {
	EgressImage       string                   `yaml:"egressImage"`
	EnvironmentPolicy environmentpolicy.Policy `yaml:"environmentPolicy"`
	IdleTimeout       time.Duration            `yaml:"idleTimeout"` // zero disables suspension between turns
	Capacity          sandboxcapacity.Policy   `yaml:"capacity"`
	Bin               string                   `yaml:"bin"`
	Image             string                   `yaml:"image"`
	Network           string                   `yaml:"network"`
	// Host is the Docker daemon sandboxes run on (DOCKER_HOST syntax, e.g.
	// tcp://sandbox-daemon:2376). Setting it enables the "sandbox"
	// readiness check. Empty = the docker CLI default (DOCKER_HOST from the
	// environment, else the local socket).
	Host string `yaml:"host"`
}

type Tools struct {
	AllowPrivateNetworks bool `yaml:"allowPrivateNetworks"`
}

type Observability struct {
	ServiceName  string `yaml:"serviceName"`
	OTLPEndpoint string `yaml:"otlpEndpoint"` // http://otel-collector:4318
	LogLevel     string `yaml:"logLevel"`
	LogFormat    string `yaml:"logFormat"` // json | text | console
	GrafanaURL   string `yaml:"grafanaUrl"`
	// TraceSampleRatio in [0,1] (default 1).
	TraceSampleRatio float64 `yaml:"traceSampleRatio"`
}

type Engine struct {
	LeaseTTL time.Duration `yaml:"leaseTTL"`
	// ReconcileInterval drives autoscaling, lease renewal and failover.
	ReconcileInterval time.Duration `yaml:"reconcileInterval"`
	// MaxConcurrentTurns caps turns across all graphs on this instance (0 = unlimited).
	MaxConcurrentTurns int `yaml:"maxConcurrentTurns"`
	// RetryBackoff is the base delay for nacked inputs (doubles per attempt).
	RetryBackoff time.Duration `yaml:"retryBackoff"`
}

type Bootstrap struct {
	Graphs []string `yaml:"graphs"`
	// Start lists graph ids to start after bootstrapping.
	Start []string `yaml:"start"`
}

// Default returns a configuration that runs on a laptop with no services:
// SQLite storage, filesystem queues, the mock model provider.
func Default() *Config {
	host, _ := os.Hostname()
	return &Config{
		Preview:  Preview{Addr: "127.0.0.1:8082", BaseURL: "http://localhost:8082", ConsoleOrigins: []string{"http://localhost:8080"}, RelayBinary: "bin/weft-preview-linux"},
		Instance: host,
		Server: Server{
			Addr:         ":8080",
			PublicURL:    "http://localhost:8080",
			InternalURL:  "http://127.0.0.1:8080",
			MaxBodyBytes: 8 << 20,
		},
		Storage:    Storage{Type: "sqlite", DSN: "file:data/weft.db"},
		Queue:      Queue{Type: "fs", Dir: "data/queues"},
		Workspaces: Workspaces{Root: "data/workspaces"},
		Secrets: Secrets{
			KeyEnv: "WEFT_SECRET_KEY", KeyFile: "data/secret.key", GenerateKeyFile: true,
			PreviousKeysEnv: "WEFT_SECRET_KEY_PREVIOUS",
		},
		Isolation: Isolation{
			Docker: DockerIsolation{EgressImage: "weft/egress:latest", IdleTimeout: 5 * time.Minute, Capacity: sandboxcapacity.DefaultPolicy(), Bin: "docker", Image: "weft/sandbox:latest", Network: "none"},
		},
		Observability: Observability{ServiceName: "weft", LogLevel: "info", LogFormat: "json", TraceSampleRatio: 1},
		Engine: Engine{
			LeaseTTL:          20 * time.Second,
			ReconcileInterval: 2 * time.Second,
			RetryBackoff:      2 * time.Second,
		},
	}
}

// Load reads path (if non-empty and present) and applies environment overrides.
func Load(path string) (*Config, error) {
	c := Default()
	if path == "" {
		path = os.Getenv("WEFT_CONFIG")
	}
	if path != "" {
		b, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		b = []byte(os.ExpandEnv(string(b)))
		if err := yaml.Unmarshal(b, c); err != nil {
			return nil, fmt.Errorf("parse config %s: %w", path, err)
		}
		var legacy struct {
			LLM map[string]any `yaml:"llm"`
		}
		if yaml.Unmarshal(b, &legacy) == nil {
			for _, k := range []string{"providers", "default"} {
				if _, ok := legacy.LLM[k]; ok {
					return nil, fmt.Errorf("parse config %s: llm.%s was removed: model providers are inference plugins enabled and configured in the console (only llm.devMock remains)", path, k)
				}
			}
		}
	}
	applyEnv(c)
	return c, c.Validate()
}

func applyEnv(c *Config) {
	str := func(key string, dst *string) {
		if v, ok := os.LookupEnv(key); ok && v != "" {
			*dst = v
		}
	}
	str("WEFT_INSTANCE", &c.Instance)
	str("WEFT_PUBLIC_URL", &c.Server.PublicURL)
	str("WEFT_PREVIEW_ADDR", &c.Preview.Addr)
	str("WEFT_PREVIEW_BASE_URL", &c.Preview.BaseURL)
	str("WEFT_PREVIEW_RELAY", &c.Preview.RelayBinary)
	if v := os.Getenv("WEFT_PREVIEW_CONTAINER_LOCAL"); v != "" {
		c.Preview.ContainerLocal, _ = strconv.ParseBool(v)
	}
	str("WEFT_ADDR", &c.Server.Addr)
	str("WEFT_INTERNAL_URL", &c.Server.InternalURL)
	str("WEFT_INTERNAL_ADDR", &c.Server.InternalAddr)
	str("WEFT_WEB_DIR", &c.Server.WebDir)
	str("WEFT_STORAGE_TYPE", &c.Storage.Type)
	str("WEFT_STORAGE_DSN", &c.Storage.DSN)
	str("WEFT_STORAGE_DIR", &c.Storage.Dir)
	str("WEFT_QUEUE_TYPE", &c.Queue.Type)
	str("WEFT_QUEUE_URL", &c.Queue.URL)
	str("WEFT_QUEUE_DIR", &c.Queue.Dir)
	str("WEFT_WORKSPACES", &c.Workspaces.Root)
	str("WEFT_WORKSPACES_HOST", &c.Workspaces.HostRoot)
	if v := os.Getenv("WEFT_DEV_MOCK"); v != "" {
		c.LLM.DevMock, _ = strconv.ParseBool(v)
	}
	str("WEFT_SANDBOX_IMAGE", &c.Isolation.Docker.Image)
	str("WEFT_SANDBOX_DOCKER_HOST", &c.Isolation.Docker.Host)
	str("WEFT_SECRET_KEY_FILE", &c.Secrets.KeyFile)
	if v := os.Getenv("WEFT_SECRET_KEY_GENERATE"); v != "" {
		c.Secrets.GenerateKeyFile, _ = strconv.ParseBool(v)
	}
	str("WEFT_LOG_LEVEL", &c.Observability.LogLevel)
	str("WEFT_LOG_FORMAT", &c.Observability.LogFormat)
	str("WEFT_GRAFANA_URL", &c.Observability.GrafanaURL)
	str("OTEL_EXPORTER_OTLP_ENDPOINT", &c.Observability.OTLPEndpoint)
	str("OTEL_SERVICE_NAME", &c.Observability.ServiceName)
	if v := os.Getenv("WEFT_PROTECT_METRICS"); v != "" {
		c.Server.ProtectMetrics, _ = strconv.ParseBool(v)
	}
	if v := os.Getenv("WEFT_AUTH_DISABLED"); v != "" {
		c.Auth.Disabled, _ = strconv.ParseBool(v)
	}
	if v := os.Getenv("WEFT_BOOTSTRAP_GRAPHS"); v != "" {
		c.Bootstrap.Graphs = splitList(v)
	}
	if v := os.Getenv("WEFT_BOOTSTRAP_START"); v != "" {
		c.Bootstrap.Start = splitList(v)
	}
	// WEFT_ADMIN_TOKEN is the quickest way to secure a deployment: one
	// admin token from the environment, no config file needed.
	if os.Getenv("WEFT_ADMIN_TOKEN") != "" {
		c.Auth.Tokens = append(c.Auth.Tokens, Token{Name: "admin", Role: RoleAdmin, TokenEnv: "WEFT_ADMIN_TOKEN"})
	}
}

func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

func (c *Config) Validate() error {
	if c.Preview.Addr != "" {
		addr := c.Preview.Addr
		if c.Preview.ContainerLocal {
			addr = "127.0.0.1:8082"
		}
		if err := review.ValidateGateway(addr, c.Preview.BaseURL); err != nil {
			return fmt.Errorf("preview: %w", err)
		}
		if strings.HasPrefix(c.Preview.BaseURL, "https://") && (c.Preview.TLSCert == "" || c.Preview.TLSKey == "") {
			return fmt.Errorf("remote preview requires tlsCert and tlsKey")
		}
	}
	if err := c.Isolation.Docker.EnvironmentPolicy.Validate(); err != nil {
		return err
	}
	if err := c.Isolation.Docker.EnvironmentPolicy.RunNetwork(c.Isolation.Docker.Network); err != nil {
		return err
	}
	if c.Isolation.Docker.IdleTimeout < 0 {
		return fmt.Errorf("isolation.docker.idleTimeout must be nonnegative")
	}
	if err := c.Isolation.Docker.Capacity.Validate(); err != nil {
		return fmt.Errorf("isolation.docker.capacity: %w", err)
	}
	if c.Instance == "" {
		c.Instance = "weft-0"
	}
	if c.Engine.LeaseTTL < 3*time.Second {
		return fmt.Errorf("engine.leaseTTL must be >= 3s")
	}
	if c.Engine.ReconcileInterval <= 0 || c.Engine.ReconcileInterval > c.Engine.LeaseTTL/2 {
		c.Engine.ReconcileInterval = c.Engine.LeaseTTL / 4
	}
	if c.Engine.RetryBackoff <= 0 {
		c.Engine.RetryBackoff = 2 * time.Second
	}
	if c.Server.MaxBodyBytes <= 0 {
		c.Server.MaxBodyBytes = 8 << 20
	}
	if !c.Auth.Disabled && len(c.Auth.Tokens) == 0 && c.Auth.OIDC == nil {
		return fmt.Errorf("no API credentials configured: set WEFT_ADMIN_TOKEN, auth.tokens, auth.oidc, or WEFT_AUTH_DISABLED=true for local development")
	}
	for _, t := range c.Auth.Tokens {
		switch t.Role {
		case RoleViewer, RoleOperator, RoleAdmin:
		default:
			return fmt.Errorf("auth token %q has invalid role %q", t.Name, t.Role)
		}
	}
	return nil
}

// ResolveToken returns the secret value of a token definition.
func (t Token) Resolve() (string, error) {
	switch {
	case t.TokenEnv != "":
		v := os.Getenv(t.TokenEnv)
		if v == "" {
			return "", fmt.Errorf("token %q: env %s is empty", t.Name, t.TokenEnv)
		}
		return v, nil
	case t.TokenFile != "":
		b, err := os.ReadFile(t.TokenFile)
		if err != nil {
			return "", fmt.Errorf("token %q: %w", t.Name, err)
		}
		return strings.TrimSpace(string(b)), nil
	case t.Token != "":
		return t.Token, nil
	}
	return "", fmt.Errorf("token %q has no value", t.Name)
}
