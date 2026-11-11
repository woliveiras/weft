package domain

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// APIVersion is the schema version of graph documents this build understands.
const APIVersion = "weft.io/v1"

// Graph is a versioned workflow definition: agents (nodes) wired together by
// channels (edges). It is a declarative document - YAML or JSON - so it can
// be reviewed, diffed and committed like any other source file.
type Graph struct {
	APIVersion string        `json:"apiVersion" yaml:"apiVersion"`
	Kind       string        `json:"kind" yaml:"kind"`
	Metadata   GraphMetadata `json:"metadata" yaml:"metadata"`
	Spec       GraphSpec     `json:"spec" yaml:"spec"`
	// Extensions holds "x-" keys, kept verbatim across saves (see extensions.go).
	Extensions Extensions `json:"-" yaml:"-"`
}

type GraphMetadata struct {
	// ID is the stable identifier (DNS-label style). Generated from Name if empty.
	ID          string            `json:"id" yaml:"id"`
	Name        string            `json:"name" yaml:"name"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty" yaml:"labels,omitempty"`
	// Version is assigned by the store on every save; documents may omit it.
	Version   int       `json:"version,omitempty" yaml:"version,omitempty"`
	CreatedAt time.Time `json:"createdAt,omitempty" yaml:"createdAt,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty" yaml:"updatedAt,omitempty"`
	// Extensions holds "x-" keys, kept verbatim across saves (see extensions.go).
	Extensions Extensions `json:"-" yaml:"-"`
}

type InstructionRefs struct {
	SkillIDs             []string `json:"skillIds,omitempty" yaml:"skillIds,omitempty"`
	ProjectSteering      bool     `json:"projectSteering,omitempty" yaml:"projectSteering,omitempty"`
	InstallationDefaults bool     `json:"installationDefaults,omitempty" yaml:"installationDefaults,omitempty"`
}

type GraphSpec struct {
	Instructions InstructionRefs `json:"instructions,omitempty" yaml:"instructions,omitempty"`

	Parameters []Parameter   `json:"parameters,omitempty" yaml:"parameters,omitempty"`
	Workspace  WorkspaceSpec `json:"workspace,omitempty" yaml:"workspace,omitempty"`
	// Environment is the container image agents' commands run in when they
	// use docker isolation without an image of their own. Weft builds it
	// on the sandbox daemon (see internal/environment).
	Environment *EnvironmentSpec `json:"environment,omitempty" yaml:"environment,omitempty"`
	Defaults    NodeDefaults     `json:"defaults,omitempty" yaml:"defaults,omitempty"`
	Nodes       []Node           `json:"nodes" yaml:"nodes"`
	Channels    []Channel        `json:"channels,omitempty" yaml:"channels,omitempty"`
	// Checks are the workflow's declared verification commands (test, lint,
	// build, custom). At task start, project checks are merged in; a
	// workflow check replaces a project check of the same name.
	Checks []Check `json:"checks,omitempty" yaml:"checks,omitempty"`
	// MaxHops bounds how many agent hops a message chain may take. Cycles are
	// legal; unbounded cycles are not. 0 means the platform default (64).
	MaxHops int `json:"maxHops,omitempty" yaml:"maxHops,omitempty"`
}

// EnvironmentOrigin identifies the repository snapshot accepted by the operator.
// Build revalidates both the source and the normalized definition.
type EnvironmentOrigin struct {
	Target string `json:"target,omitempty" yaml:"target,omitempty"`
	// DockerfilePath selects a Dockerfile-specific ignore file when present.
	DockerfilePath string `json:"dockerfilePath,omitempty" yaml:"dockerfilePath,omitempty"`
	ContextSHA256  string `json:"contextSHA256,omitempty" yaml:"contextSHA256,omitempty"`

	Source  string            `json:"source" yaml:"source"`
	Path    string            `json:"path" yaml:"path"`
	SHA256  string            `json:"sha256" yaml:"sha256"`
	Service string            `json:"service,omitempty" yaml:"service,omitempty"`
	Files   map[string]string `json:"files" yaml:"files"`
}

// EnvironmentSpec defines a project environment: a base image (or a
// Dockerfile), setup commands run once with the workspace mounted, and the
// network and cache policy of the containers built from it.
type EnvironmentSpec struct {
	Companions []Companion `json:"companions,omitempty" yaml:"companions,omitempty"`
	// Target selects a named Dockerfile stage; empty builds the final stage.
	Target string             `json:"target,omitempty" yaml:"target,omitempty"`
	Origin *EnvironmentOrigin `json:"origin,omitempty" yaml:"origin,omitempty"`

	// Image is a base image to pull (e.g. "golang:1.26"). Exactly one of
	// Image and Dockerfile is set.
	Image string `json:"image,omitempty" yaml:"image,omitempty"`
	// Context is a repository-relative directory, captured and fingerprinted
	// during repository acceptance. Empty retains the inline-only build path.
	Context string `json:"context,omitempty" yaml:"context,omitempty"`
	// Dockerfile is the accepted inline build definition.
	Dockerfile string `json:"dockerfile,omitempty" yaml:"dockerfile,omitempty"`
	// Setup commands run in order, as the sandbox user, in /workspace with
	// the graph workspace mounted; their effect on the image (installed
	// tools, downloaded dependencies outside the workspace) is kept.
	Setup []string `json:"setup,omitempty" yaml:"setup,omitempty"`
	// Network per phase: "none" or "bridge" (internet only).
	Network EnvironmentNetwork `json:"network,omitempty" yaml:"network,omitempty"`
	// Caches are absolute directories kept in named volumes across sandbox
	// containers and rebuilds (e.g. the Go module cache).
	Caches []string `json:"caches,omitempty" yaml:"caches,omitempty"`
	// Timeout bounds the whole build (default 30m, at most 2h).
	Timeout Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	// CPU and Memory limit the setup container.
	CPU    string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"`
}

// EnvironmentNetwork is the network of each environment phase.
type EnvironmentNetwork struct {
	SetupAllowedHosts []string `json:"setupAllowedHosts,omitempty" yaml:"setupAllowedHosts,omitempty"`
	AllowedHosts      []string `json:"allowedHosts,omitempty" yaml:"allowedHosts,omitempty"`
	// Setup defaults to "bridge": installing tools needs the internet.
	Setup string `json:"setup,omitempty" yaml:"setup,omitempty"`
	// Run is the default for agent sandboxes ("none"); a node's
	// isolation.network overrides it.
	Run string `json:"run,omitempty" yaml:"run,omitempty"`
}

// Parameter is a graph-wide value substituted into prompts as @name.
type Parameter struct {
	Name        string `json:"name" yaml:"name"`
	Value       string `json:"value" yaml:"value"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
}

// WorkspaceSpec is the directory tree the agents of a graph work in.
type WorkspaceSpec struct {
	// Path is relative to the platform workspace root, or absolute.
	// Empty means <root>/<graph-id>.
	Path string `json:"path,omitempty" yaml:"path,omitempty"`
	// Git initialises the workspace as a repository when it is not one.
	Git bool `json:"git,omitempty" yaml:"git,omitempty"`
	// Repo is cloned into the workspace on first start when set.
	Repo string `json:"repo,omitempty" yaml:"repo,omitempty"`
}

// NodeDefaults apply to every node that leaves the field unset.
type NodeDefaults struct {
	Runtime   RuntimeRef    `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Model     ModelRef      `json:"model,omitempty" yaml:"model,omitempty"`
	Tools     []ToolGrant   `json:"tools,omitempty" yaml:"tools,omitempty"`
	Isolation IsolationSpec `json:"isolation,omitempty" yaml:"isolation,omitempty"`
	Loop      *LoopSpec     `json:"loop,omitempty" yaml:"loop,omitempty"`
}

// Node is an agent placed in the graph.
type Node struct {
	SkillIDs SkillSelection `json:"skillIds" yaml:"skillIds"`

	ID          string        `json:"id" yaml:"id"`
	Name        string        `json:"name,omitempty" yaml:"name,omitempty"`
	Description string        `json:"description,omitempty" yaml:"description,omitempty"`
	Runtime     RuntimeRef    `json:"runtime,omitempty" yaml:"runtime,omitempty"`
	Model       ModelRef      `json:"model,omitempty" yaml:"model,omitempty"`
	Prompt      string        `json:"prompt,omitempty" yaml:"prompt,omitempty"`
	Tools       []ToolGrant   `json:"tools,omitempty" yaml:"tools,omitempty"`
	Loop        LoopSpec      `json:"loop,omitempty" yaml:"loop,omitempty"`
	Cluster     ClusterSpec   `json:"cluster,omitempty" yaml:"cluster,omitempty"`
	Isolation   IsolationSpec `json:"isolation,omitempty" yaml:"isolation,omitempty"`
	Workspace   NodeWorkspace `json:"workspace,omitempty" yaml:"workspace,omitempty"`
	Approval    ApprovalSpec  `json:"approval,omitempty" yaml:"approval,omitempty"`
	// Subscribe lists topic names (wildcards allowed: "research.*") this node
	// consumes without the producer having to name it.
	Subscribe []string `json:"subscribe,omitempty" yaml:"subscribe,omitempty"`
	// Limits per turn.
	Limits NodeLimits `json:"limits,omitempty" yaml:"limits,omitempty"`
	// Disabled nodes are skipped by every start, but their queues keep
	// collecting work.
	Disabled bool `json:"disabled,omitempty" yaml:"disabled,omitempty"`
	// Config is passed verbatim to the runtime (framework-specific settings).
	Config map[string]any `json:"config,omitempty" yaml:"config,omitempty"`
	// Position is UI layout only.
	Position *Position `json:"position,omitempty" yaml:"position,omitempty"`
	// Extensions holds "x-" keys, kept verbatim across saves (see extensions.go).
	Extensions Extensions `json:"-" yaml:"-"`
}

type Position struct {
	X float64 `json:"x" yaml:"x"`
	Y float64 `json:"y" yaml:"y"`
}

func (n *Node) DisplayName() string {
	if n.Name != "" {
		return n.Name
	}
	return n.ID
}

// RuntimeKind selects how a node's turns are executed.
type RuntimeKind string

const (
	// RuntimeNative is the built-in Go agent: an LLM tool-calling loop whose
	// tools are executed by the platform's tool gateway.
	RuntimeNative RuntimeKind = "native"
	// RuntimeExternal delegates turns over the Runtime Protocol (HTTP/JSON) to
	// an agent process written in any language/framework (LangGraph, AutoGen,
	// CrewAI, custom). Tools and LLM calls still go through the gateway.
	RuntimeExternal RuntimeKind = "external"
	// RuntimeEcho forwards inputs to outputs without calling a model. It
	// exercises the whole machinery for free.
	RuntimeEcho RuntimeKind = "echo"
)

type RuntimeRef struct {
	Kind RuntimeKind `json:"kind,omitempty" yaml:"kind,omitempty"`
	// Framework is informational for external runtimes: langgraph, autogen,
	// crewai, custom. The protocol is identical for all of them.
	Framework string `json:"framework,omitempty" yaml:"framework,omitempty"`
	// Agent is the name the external process registered the agent under.
	Agent string `json:"agent,omitempty" yaml:"agent,omitempty"`
	// Endpoint of an already-running external runtime (http://agents:8000).
	// When empty and Command/Image is set the platform launches one.
	Endpoint string `json:"endpoint,omitempty" yaml:"endpoint,omitempty"`
	// Command launches a runtime process (process isolation).
	Command []string `json:"command,omitempty" yaml:"command,omitempty"`
	// Image launches a runtime container (docker/kubernetes isolation).
	Image string            `json:"image,omitempty" yaml:"image,omitempty"`
	Env   map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

// ModelRef names a provider registered in platform config plus a model.
type ModelRef struct {
	Provider    string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Name        string   `json:"name,omitempty" yaml:"name,omitempty"`
	Temperature *float64 `json:"temperature,omitempty" yaml:"temperature,omitempty"`
	MaxTokens   int      `json:"maxTokens,omitempty" yaml:"maxTokens,omitempty"`
}

// ToolGrant gives a node one capability, optionally narrowed. In YAML a grant
// may be written as a bare string ("filesystem.read") or as an object.
type ToolGrant struct {
	// Name is a capability ("filesystem.write"), a tool name, or a wildcard
	// ("git.*", "mcp.github.*", "*").
	Name string `json:"name" yaml:"name"`
	// Paths restricts filesystem capabilities to globs relative to the
	// workspace ("src/**", "docs/*.md"). Empty means the whole workspace.
	Paths []string `json:"paths,omitempty" yaml:"paths,omitempty"`
	// Commands restricts shell.exec to argv[0]/glob patterns ("go", "npm *").
	Commands []string `json:"commands,omitempty" yaml:"commands,omitempty"`
	// Hosts restricts http.fetch to host globs ("*.wikipedia.org").
	Hosts []string `json:"hosts,omitempty" yaml:"hosts,omitempty"`
	// Repositories names the SCM connections git.push and pull request
	// tools may use. Unlike the other axes, empty grants none.
	Repositories []string `json:"repositories,omitempty" yaml:"repositories,omitempty"`
	// RequiresApproval makes every invocation wait for a human decision.
	RequiresApproval bool `json:"requiresApproval,omitempty" yaml:"requiresApproval,omitempty"`
	// RateLimit is the maximum invocations per minute (0 = unlimited).
	RateLimit int `json:"rateLimit,omitempty" yaml:"rateLimit,omitempty"`
}

func (g *ToolGrant) UnmarshalYAML(n *yaml.Node) error {
	if n.Kind == yaml.ScalarNode {
		g.Name = n.Value
		return nil
	}
	type plain ToolGrant
	return n.Decode((*plain)(g))
}

func (g *ToolGrant) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err == nil {
		g.Name = s
		return nil
	}
	type plain ToolGrant
	return json.Unmarshal(b, (*plain)(g))
}

// LoopMode decides when a node takes a turn.
type LoopMode string

const (
	// LoopOnce takes exactly one turn per start, then completes.
	LoopOnce LoopMode = "once"
	// LoopContinuous takes turn after turn, with or without input.
	LoopContinuous LoopMode = "continuous"
	// LoopScheduled takes a turn on a cron or @every schedule.
	LoopScheduled LoopMode = "scheduled"
	// LoopEvent takes a turn only when a message (or operator message) is
	// waiting, sleeping otherwise.
	LoopEvent LoopMode = "event"
)

// StopPolicy decides when a running loop ends by itself.
type StopPolicy string

const (
	StopNever      StopPolicy = "never"      // until an operator stops it
	StopAuto       StopPolicy = "auto"       // agent declares done, confirms next turn
	StopIterations StopPolicy = "iterations" // after MaxIterations turns this run
	StopDuration   StopPolicy = "duration"   // after MaxDuration this run
	StopIdle       StopPolicy = "idle"       // after IdleTimeout with no input
)

type LoopSpec struct {
	Mode LoopMode `json:"mode,omitempty" yaml:"mode,omitempty"`
	// Schedule for LoopScheduled: cron ("*/5 * * * *") or "@every 1h".
	Schedule string `json:"schedule,omitempty" yaml:"schedule,omitempty"`
	// Interval is a pause between turns (pacing), in any mode.
	Interval Duration `json:"interval,omitempty" yaml:"interval,omitempty"`
	Stop     StopSpec `json:"stop,omitempty" yaml:"stop,omitempty"`
	// BatchSize is the max number of input messages claimed per turn (default 1).
	BatchSize int `json:"batchSize,omitempty" yaml:"batchSize,omitempty"`
}

type StopSpec struct {
	Policy        StopPolicy `json:"policy,omitempty" yaml:"policy,omitempty"`
	MaxIterations int        `json:"maxIterations,omitempty" yaml:"maxIterations,omitempty"`
	MaxDuration   Duration   `json:"maxDuration,omitempty" yaml:"maxDuration,omitempty"`
	IdleTimeout   Duration   `json:"idleTimeout,omitempty" yaml:"idleTimeout,omitempty"`
}

// ClusterMode decides how many replicas a node runs.
type ClusterMode string

const (
	ClusterFixed     ClusterMode = "fixed"
	ClusterAutoscale ClusterMode = "autoscale"
)

// ClusterComms decides whether replicas see each other.
type ClusterComms string

const (
	// CommsIsolated replicas share only the queue they compete over.
	CommsIsolated ClusterComms = "isolated"
	// CommsRing replicas read their two ring neighbours' last broadcast
	// before each turn.
	CommsRing ClusterComms = "ring"
)

type ClusterSpec struct {
	Mode     ClusterMode `json:"mode,omitempty" yaml:"mode,omitempty"`
	Replicas int         `json:"replicas,omitempty" yaml:"replicas,omitempty"`
	// MinReplicas/MaxReplicas bound autoscaling. Autoscale targets one
	// replica per pending message, clamped to [Min, Max].
	MinReplicas int          `json:"minReplicas,omitempty" yaml:"minReplicas,omitempty"`
	MaxReplicas int          `json:"maxReplicas,omitempty" yaml:"maxReplicas,omitempty"`
	Comms       ClusterComms `json:"comms,omitempty" yaml:"comms,omitempty"`
	// ScaleDownAfter is how long a replica must be idle before it is shed.
	ScaleDownAfter Duration `json:"scaleDownAfter,omitempty" yaml:"scaleDownAfter,omitempty"`
}

// IsolationMode decides where an agent's tools and external runtime execute.
type IsolationMode string

const (
	IsolationProcess    IsolationMode = "process"
	IsolationDocker     IsolationMode = "docker"
	IsolationKubernetes IsolationMode = "kubernetes"
)

type IsolationSpec struct {
	Mode  IsolationMode `json:"mode,omitempty" yaml:"mode,omitempty"`
	Image string        `json:"image,omitempty" yaml:"image,omitempty"`
	// CPU in cores ("0.5"), Memory as a docker/k8s quantity ("512Mi").
	CPU    string `json:"cpu,omitempty" yaml:"cpu,omitempty"`
	Memory string `json:"memory,omitempty" yaml:"memory,omitempty"`
	// Network: "none" (default for docker sandboxes) or "bridge"/"egress".
	Network string `json:"network,omitempty" yaml:"network,omitempty"`
	// Env is passed to the sandbox; secrets are never inherited implicitly.
	Env map[string]string `json:"env,omitempty" yaml:"env,omitempty"`
}

// WorkspaceMode decides whether replicas share a directory.
type WorkspaceMode string

const (
	WorkspaceShared   WorkspaceMode = "shared"
	WorkspaceWorktree WorkspaceMode = "worktree" // git worktree + branch per replica
)

type NodeWorkspace struct {
	// ReadOnly denies mutation tools and shell execution, even with broader grants.
	ReadOnly bool          `json:"readOnly,omitempty" yaml:"readOnly,omitempty"`
	Mode     WorkspaceMode `json:"mode,omitempty" yaml:"mode,omitempty"`
	// Subdir narrows the node's view of the graph workspace.
	Subdir string `json:"subdir,omitempty" yaml:"subdir,omitempty"`
}

// ApprovalSpec places human gates on a node.
type ApprovalSpec struct {
	// BeforeTurn requires approval before every turn starts.
	BeforeTurn bool `json:"beforeTurn,omitempty" yaml:"beforeTurn,omitempty"`
	// BeforeEmit holds every output message until approved.
	BeforeEmit bool `json:"beforeEmit,omitempty" yaml:"beforeEmit,omitempty"`
	// Timeout after which a pending approval resolves to OnTimeout.
	Timeout   Duration `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	OnTimeout string   `json:"onTimeout,omitempty" yaml:"onTimeout,omitempty"` // reject (default) | approve
}

type NodeLimits struct {
	TurnTimeout  Duration `json:"turnTimeout,omitempty" yaml:"turnTimeout,omitempty"`
	MaxToolCalls int      `json:"maxToolCalls,omitempty" yaml:"maxToolCalls,omitempty"`
	MaxTokens    int      `json:"maxTokens,omitempty" yaml:"maxTokens,omitempty"`
}

// ChannelType is the delivery semantics of an edge.
type ChannelType string

const (
	// ChannelQueue: point-to-point. Every consumer (all target nodes and all
	// their replicas) competes over one backlog; each message is processed once.
	ChannelQueue ChannelType = "queue"
	// ChannelTopic: publish-subscribe on a named topic. Every subscribing node
	// gets its own copy; replicas of one node compete over that copy.
	// Subscribers may be listed in To or opt in via Node.Subscribe.
	ChannelTopic ChannelType = "topic"
	// ChannelFanout: the producer delivers a copy to each listed target node
	// (static pub-sub). Replicas of a target compete.
	ChannelFanout ChannelType = "fanout"
	// ChannelBroadcast: every replica of every target node gets its own copy.
	ChannelBroadcast ChannelType = "broadcast"
)

// Channel is a durable edge between agents.
type Channel struct {
	ID   string      `json:"id" yaml:"id"`
	Type ChannelType `json:"type,omitempty" yaml:"type,omitempty"`
	// From is the producing node. Empty means external ingress only (API/injection).
	From string `json:"from,omitempty" yaml:"from,omitempty"`
	// To lists consuming nodes.
	To []string `json:"to,omitempty" yaml:"to,omitempty"`
	// Topic is the topic name for ChannelTopic (defaults to ID).
	Topic string `json:"topic,omitempty" yaml:"topic,omitempty"`
	// Types restricts which message types are routed onto this channel
	// (exact or glob, e.g. "review.*"). Empty means all.
	Types []string `json:"types,omitempty" yaml:"types,omitempty"`
	// When is a boolean expression over the message (dynamic branching), e.g.
	//   payload.verdict == "approved" && metadata.hops < 10
	When string `json:"when,omitempty" yaml:"when,omitempty"`
	// Approval holds every message published on this channel at a human gate.
	Approval bool `json:"approval,omitempty" yaml:"approval,omitempty"`
	// Delivery tuning.
	MaxDeliver int      `json:"maxDeliver,omitempty" yaml:"maxDeliver,omitempty"` // default 5
	AckWait    Duration `json:"ackWait,omitempty" yaml:"ackWait,omitempty"`       // default 10m
	Retention  Duration `json:"retention,omitempty" yaml:"retention,omitempty"`   // default 7d
	Label      string   `json:"label,omitempty" yaml:"label,omitempty"`
	// Extensions holds "x-" keys, kept verbatim across saves (see extensions.go).
	Extensions Extensions `json:"-" yaml:"-"`
}

// TopicName is the logical topic for topic channels.
func (c *Channel) TopicName() string {
	if c.Topic != "" {
		return c.Topic
	}
	return c.ID
}

// Duration marshals as a Go duration string ("90s", "2h") in YAML and JSON,
// and also accepts plain numbers of seconds.
type Duration time.Duration

func (d Duration) D() time.Duration { return time.Duration(d) }

func (d Duration) String() string { return time.Duration(d).String() }

func (d Duration) MarshalJSON() ([]byte, error) {
	if d == 0 {
		return []byte(`""`), nil
	}
	return json.Marshal(time.Duration(d).String())
}

func (d *Duration) UnmarshalJSON(b []byte) error {
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	return d.set(v)
}

func (d Duration) MarshalYAML() (any, error) {
	if d == 0 {
		return "", nil
	}
	return time.Duration(d).String(), nil
}

func (d *Duration) UnmarshalYAML(n *yaml.Node) error {
	var v any
	if err := n.Decode(&v); err != nil {
		return err
	}
	return d.set(v)
}

func (d *Duration) set(v any) error {
	switch t := v.(type) {
	case nil:
		*d = 0
	case float64:
		*d = Duration(time.Duration(t * float64(time.Second)))
	case int:
		*d = Duration(time.Duration(t) * time.Second)
	case string:
		if strings.TrimSpace(t) == "" {
			*d = 0
			return nil
		}
		p, err := time.ParseDuration(t)
		if err != nil {
			return fmt.Errorf("invalid duration %q: %w", t, err)
		}
		*d = Duration(p)
	default:
		return fmt.Errorf("invalid duration %v", v)
	}
	return nil
}

// Node lookup helpers.

func (g *Graph) Node(id string) *Node {
	for i := range g.Spec.Nodes {
		if g.Spec.Nodes[i].ID == id {
			return &g.Spec.Nodes[i]
		}
	}
	return nil
}

func (g *Graph) Channel(id string) *Channel {
	for i := range g.Spec.Channels {
		if g.Spec.Channels[i].ID == id {
			return &g.Spec.Channels[i]
		}
	}
	return nil
}

// Outgoing returns channels the node produces onto.
func (g *Graph) Outgoing(nodeID string) []*Channel {
	var out []*Channel
	for i := range g.Spec.Channels {
		if g.Spec.Channels[i].From == nodeID {
			out = append(out, &g.Spec.Channels[i])
		}
	}
	return out
}

// Incoming returns channels the node consumes from, including topics it
// subscribes to by pattern.
func (g *Graph) Incoming(nodeID string) []*Channel {
	n := g.Node(nodeID)
	var out []*Channel
	for i := range g.Spec.Channels {
		c := &g.Spec.Channels[i]
		if contains(c.To, nodeID) {
			out = append(out, c)
			continue
		}
		if n != nil && c.Type == ChannelTopic {
			for _, pat := range n.Subscribe {
				if GlobMatch(pat, c.TopicName()) {
					out = append(out, c)
					break
				}
			}
		}
	}
	return out
}

// Consumers returns every node consuming from a channel (explicit + subscribers).
func (g *Graph) Consumers(c *Channel) []string {
	out := append([]string(nil), c.To...)
	if c.Type == ChannelTopic {
		for _, n := range g.Spec.Nodes {
			if contains(out, n.ID) {
				continue
			}
			for _, pat := range n.Subscribe {
				if GlobMatch(pat, c.TopicName()) {
					out = append(out, n.ID)
					break
				}
			}
		}
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// GlobMatch matches name against a pattern where '*' matches any run of
// characters (including dots) and '?' a single character. It is used for
// tool grants, message types and topic subscriptions, so it is deliberately
// simpler than path globbing.
func GlobMatch(pattern, name string) bool {
	if pattern == "*" || pattern == name {
		return true
	}
	p, n := []rune(pattern), []rune(name)
	var pi, ni, star, match = 0, 0, -1, 0
	for ni < len(n) {
		switch {
		case pi < len(p) && (p[pi] == '?' || p[pi] == n[ni]):
			pi++
			ni++
		case pi < len(p) && p[pi] == '*':
			star, match = pi, ni
			pi++
		case star != -1:
			pi = star + 1
			match++
			ni = match
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == '*' {
		pi++
	}
	return pi == len(p)
}
