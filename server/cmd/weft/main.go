// Command weft is the Weft command-line client.
//
//	WEFT_URL (default http://localhost:8080) and WEFT_TOKEN select the server.
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
)

const usage = `weft - Weft agent factory CLI

Graphs:
  weft graphs                         list graphs
  weft apply -f graph.yaml            create or update a graph
  weft validate -f graph.yaml         validate a graph locally and on the server
  weft get <graph> [-o yaml|json]     print a graph document
  weft delete <graph>                 delete a (stopped) graph
  weft status <graph>                 replicas, states and queue depths

Lifecycle:
  weft start|stop|pause|resume|kill <graph> [--node <node>]
  weft scale <graph> <node> <replicas>

Work and humans:
  weft inject <graph> <channel> <type> [payload-json|@file]
  weft chat <graph> <node> <text> [--replica n]
  weft approvals [--graph g]          pending approvals
  weft approve|reject <approval-id> [--comment c] [--answer a]
  weft messages <graph> [--channel c] [--correlation id]
  weft events <graph> [--follow]

Deployment:
  weft k8s render -f graph.yaml [--namespace ns]  manifests for external agent runtimes

Environment: WEFT_URL (default http://localhost:8080), WEFT_TOKEN.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Print(usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	c := newClient()
	var err error
	switch cmd {
	case "graphs", "ls":
		err = c.listGraphs()
	case "apply":
		err = c.apply(args)
	case "validate":
		err = c.validate(args)
	case "get":
		err = c.get(args)
	case "delete":
		err = need(args, 1, func() error { return c.do("DELETE", "/api/v1/graphs/"+args[0], nil, nil) })
	case "status":
		err = need(args, 1, func() error { return c.status(args[0]) })
	case "start", "stop", "pause", "resume", "kill":
		err = c.action(cmd, args)
	case "scale":
		err = need(args, 3, func() error {
			var n int
			fmt.Sscan(args[2], &n)
			return c.do("PUT", "/api/v1/graphs/"+args[0]+"/nodes/"+args[1]+"/scale", map[string]int{"replicas": n}, nil)
		})
	case "inject":
		err = c.inject(args)
	case "chat":
		err = c.chat(args)
	case "approvals":
		err = c.approvals(args)
	case "approve", "reject":
		err = c.decide(cmd, args)
	case "messages":
		err = c.messages(args)
	case "events":
		err = c.events(args)
	case "k8s":
		err = k8sCmd(args)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		err = fmt.Errorf("unknown command %q (weft help)", cmd)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func need(args []string, n int, f func() error) error {
	if len(args) < n {
		return fmt.Errorf("expected %d argument(s)", n)
	}
	return f()
}

// --- HTTP client -------------------------------------------------------------

type client struct {
	base  string
	token string
	http  *http.Client
}

func newClient() *client {
	base := os.Getenv("WEFT_URL")
	if base == "" {
		base = "http://localhost:8080"
	}
	return &client{base: strings.TrimRight(base, "/"), token: os.Getenv("WEFT_TOKEN"), http: &http.Client{Timeout: 60 * time.Second}}
}

func (c *client) req(method, path string, body io.Reader, ctype string) (*http.Response, error) {
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		return nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	return c.http.Do(req)
}

func (c *client) do(method, path string, in, out any) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	resp, err := c.req(method, path, body, "application/json")
	if err != nil {
		return err
	}
	return decode(resp, out)
}

func decode(resp *http.Response, out any) error {
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode/100 != 2 {
		var e struct {
			Error   string          `json:"error"`
			Details json.RawMessage `json:"details"`
		}
		if json.Unmarshal(b, &e) == nil && e.Error != "" {
			if len(e.Details) > 0 && string(e.Details) != "null" {
				return fmt.Errorf("%s (HTTP %d)\n%s", e.Error, resp.StatusCode, e.Details)
			}
			return fmt.Errorf("%s (HTTP %d)", e.Error, resp.StatusCode)
		}
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil && len(b) > 0 {
		return json.Unmarshal(b, out)
	}
	return nil
}

func table(header string, rows [][]string) {
	w := tabwriter.NewWriter(os.Stdout, 0, 2, 2, ' ', 0)
	fmt.Fprintln(w, header)
	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}
	w.Flush()
}

// --- commands ----------------------------------------------------------------

type summary struct {
	Graph  domain.Graph       `json:"graph"`
	Record domain.GraphRecord `json:"record"`
}

func (c *client) listGraphs() error {
	var out []summary
	if err := c.do("GET", "/api/v1/graphs", nil, &out); err != nil {
		return err
	}
	var rows [][]string
	for _, s := range out {
		rows = append(rows, []string{s.Graph.Metadata.ID, s.Graph.Metadata.Name, string(s.Record.State), fmt.Sprint(s.Record.Version), fmt.Sprint(len(s.Graph.Spec.Nodes))})
	}
	table("ID\tNAME\tSTATE\tVERSION\tNODES", rows)
	return nil
}

func fileFlag(args []string) (string, []string) {
	fs := flag.NewFlagSet("f", flag.ContinueOnError)
	f := fs.String("f", "", "file")
	_ = fs.Parse(args)
	return *f, fs.Args()
}

func readGraph(path string) ([]byte, *domain.Graph, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	var g domain.Graph
	if err := yaml.Unmarshal(b, &g); err != nil {
		return nil, nil, err
	}
	return b, &g, nil
}

func (c *client) apply(args []string) error {
	path, _ := fileFlag(args)
	if path == "" {
		return fmt.Errorf("apply needs -f <file>")
	}
	b, g, err := readGraph(path)
	if err != nil {
		return err
	}
	graph.Normalize(g)
	var out summary
	resp, err := c.req("GET", "/api/v1/graphs/"+g.Metadata.ID, nil, "")
	if err != nil {
		return err
	}
	resp.Body.Close()
	method, p := "POST", "/api/v1/graphs"
	if resp.StatusCode == 200 {
		method, p = "PUT", "/api/v1/graphs/"+g.Metadata.ID
	}
	resp, err = c.req(method, p, bytes.NewReader(b), "application/yaml")
	if err != nil {
		return err
	}
	if err := decode(resp, &out); err != nil {
		return err
	}
	fmt.Printf("graph %s applied (version %d)\n", out.Graph.Metadata.ID, out.Record.Version)
	return nil
}

func (c *client) validate(args []string) error {
	path, _ := fileFlag(args)
	if path == "" {
		return fmt.Errorf("validate needs -f <file>")
	}
	b, g, err := readGraph(path)
	if err != nil {
		return err
	}
	rep := graph.Validate(g, nil)
	if resp, err := c.req("POST", "/api/v1/graphs/validate", bytes.NewReader(b), "application/yaml"); err == nil {
		var srv graph.Report
		if decode(resp, &srv) == nil {
			rep = &srv
		}
	}
	for _, e := range rep.Errors {
		fmt.Printf("ERROR   %s: %s\n", e.Path, e.Message)
	}
	for _, w := range rep.Warnings {
		fmt.Printf("WARNING %s: %s\n", w.Path, w.Message)
	}
	if !rep.Valid {
		return fmt.Errorf("graph is invalid")
	}
	fmt.Println("graph is valid")
	return nil
}

func (c *client) get(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("get needs a graph id")
	}
	format := "yaml"
	if len(args) >= 3 && args[1] == "-o" {
		format = args[2]
	}
	resp, err := c.req("GET", "/api/v1/graphs/"+args[0]+"/export?format="+format, nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return decode(resp, nil)
	}
	_, err = io.Copy(os.Stdout, resp.Body)
	return err
}

type status struct {
	GraphID, State, DesiredState, RunID string
	Version, ApprovalsPending           int
	Nodes                               []struct {
		NodeID, Name, State string
		DesiredReplicas     int
		Pending             int64
		Replicas            []domain.ReplicaStatus
	}
	Channels []struct {
		ChannelID, Type, From string
		To                    []string
		Groups                []struct {
			Group, Node             string
			Pending, InFlight, Dead int64
		}
	}
}

func (c *client) status(id string) error {
	var st status
	if err := c.do("GET", "/api/v1/graphs/"+id+"/status", nil, &st); err != nil {
		return err
	}
	fmt.Printf("graph %s  state=%s desired=%s version=%d run=%s approvals=%d\n\n", st.GraphID, st.State, st.DesiredState, st.Version, st.RunID, st.ApprovalsPending)
	var rows [][]string
	for _, n := range st.Nodes {
		states := []string{}
		for _, r := range n.Replicas {
			states = append(states, fmt.Sprintf("n%d:%s(#%d)", r.Replica, r.State, r.Iteration))
		}
		rows = append(rows, []string{n.NodeID, n.State, fmt.Sprint(n.Pending), strings.Join(states, " ")})
	}
	table("NODE\tSTATE\tPENDING\tREPLICAS", rows)
	fmt.Println()
	rows = nil
	for _, ch := range st.Channels {
		for _, g := range ch.Groups {
			rows = append(rows, []string{ch.ChannelID, ch.Type, ch.From + " → " + strings.Join(ch.To, ","), g.Group, fmt.Sprint(g.Pending), fmt.Sprint(g.InFlight), fmt.Sprint(g.Dead)})
		}
	}
	table("CHANNEL\tTYPE\tEDGE\tGROUP\tPENDING\tINFLIGHT\tDEAD", rows)
	return nil
}

func (c *client) action(action string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%s needs a graph id", action)
	}
	id := args[0]
	fs := flag.NewFlagSet(action, flag.ContinueOnError)
	node := fs.String("node", "", "node")
	_ = fs.Parse(args[1:])
	p := "/api/v1/graphs/" + id + "/" + action
	if *node != "" {
		p = "/api/v1/graphs/" + id + "/nodes/" + *node + "/" + action
	}
	if err := c.do("POST", p, nil, nil); err != nil {
		return err
	}
	fmt.Printf("%s: %s\n", action, id)
	return nil
}

func (c *client) inject(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("inject <graph> <channel> <type> [payload-json|@file]")
	}
	payload := json.RawMessage(`{}`)
	if len(args) >= 4 {
		p := args[3]
		if strings.HasPrefix(p, "@") {
			b, err := os.ReadFile(p[1:])
			if err != nil {
				return err
			}
			p = string(b)
		}
		if !json.Valid([]byte(p)) {
			return fmt.Errorf("payload is not valid JSON")
		}
		payload = json.RawMessage(p)
	}
	var m domain.Message
	if err := c.do("POST", "/api/v1/graphs/"+args[0]+"/channels/"+args[1]+"/messages", map[string]any{"type": args[2], "payload": payload}, &m); err != nil {
		return err
	}
	fmt.Printf("injected %s (correlation %s)\n", m.ID, m.MetaString(domain.MetaCorrelationID))
	return nil
}

func (c *client) chat(args []string) error {
	if len(args) < 3 {
		return fmt.Errorf("chat <graph> <node> <text> [--replica n]")
	}
	fs := flag.NewFlagSet("chat", flag.ContinueOnError)
	rep := fs.Int("replica", -1, "replica")
	_ = fs.Parse(args[3:])
	return c.do("POST", "/api/v1/graphs/"+args[0]+"/nodes/"+args[1]+"/chat", map[string]any{"text": args[2], "replica": *rep}, nil)
}

func (c *client) approvals(args []string) error {
	fs := flag.NewFlagSet("approvals", flag.ContinueOnError)
	g := fs.String("graph", "", "graph")
	_ = fs.Parse(args)
	var out []domain.Approval
	if err := c.do("GET", "/api/v1/approvals?status=pending&graph="+url.QueryEscape(*g), nil, &out); err != nil {
		return err
	}
	var rows [][]string
	for _, a := range out {
		rows = append(rows, []string{a.ID, a.GraphID, a.NodeID, string(a.Kind), a.Title, a.CreatedAt.Format(time.RFC3339)})
	}
	table("ID\tGRAPH\tNODE\tKIND\tTITLE\tCREATED", rows)
	return nil
}

func (c *client) decide(decision string, args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("%s <approval-id>", decision)
	}
	fs := flag.NewFlagSet(decision, flag.ContinueOnError)
	comment := fs.String("comment", "", "comment")
	answer := fs.String("answer", "", "answer (questions) or edited payload JSON (messages)")
	_ = fs.Parse(args[1:])
	var a domain.Approval
	if err := c.do("POST", "/api/v1/approvals/"+args[0]+"/"+decision, map[string]string{"comment": *comment, "answer": *answer}, &a); err != nil {
		return err
	}
	fmt.Printf("%s %s\n", a.ID, a.Status)
	return nil
}

func (c *client) messages(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("messages <graph>")
	}
	fs := flag.NewFlagSet("messages", flag.ContinueOnError)
	ch := fs.String("channel", "", "channel")
	corr := fs.String("correlation", "", "correlation id")
	_ = fs.Parse(args[1:])
	q := url.Values{"channel": {*ch}, "correlationId": {*corr}, "limit": {"50"}}
	var out []domain.MessageRecord
	if err := c.do("GET", "/api/v1/graphs/"+args[0]+"/messages?"+q.Encode(), nil, &out); err != nil {
		return err
	}
	var rows [][]string
	for _, r := range out {
		rows = append(rows, []string{r.UpdatedAt.Format("15:04:05"), r.Message.ID, r.Channel, r.Group, r.Message.Type, string(r.Status), fmt.Sprint(r.Attempt)})
	}
	table("TIME\tID\tCHANNEL\tGROUP\tTYPE\tSTATUS\tATTEMPT", rows)
	return nil
}

func (c *client) events(args []string) error {
	if len(args) < 1 {
		return fmt.Errorf("events <graph> [--follow]")
	}
	fs := flag.NewFlagSet("events", flag.ContinueOnError)
	follow := fs.Bool("follow", false, "stream")
	_ = fs.Parse(args[1:])
	print := func(e domain.Event) {
		who := e.NodeID
		if who != "" {
			who = fmt.Sprintf("%s#%d", e.NodeID, e.Replica)
		}
		fmt.Printf("%s %-24s %-16s %s\n", e.Time.Local().Format("15:04:05"), e.Type, who, truncate(string(e.Data), 160))
	}
	if !*follow {
		var out []domain.Event
		if err := c.do("GET", "/api/v1/graphs/"+args[0]+"/events?limit=100", nil, &out); err != nil {
			return err
		}
		for _, e := range out {
			print(e)
		}
		return nil
	}
	c.http.Timeout = 0
	resp, err := c.req("GET", "/api/v1/events/stream?graph="+url.QueryEscape(args[0]), nil, "")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return decode(resp, nil)
	}
	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "data: ") {
			var e domain.Event
			if json.Unmarshal([]byte(line[6:]), &e) == nil {
				print(e)
			}
		}
	}
	return sc.Err()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// --- offline commands ----------------------------------------------------------

// k8sCmd renders Deployments and Services for every external agent runtime
// with an image, so that kubernetes-isolated nodes have an endpoint to call.
func k8sCmd(args []string) error {
	if len(args) < 1 || args[0] != "render" {
		return fmt.Errorf("k8s render -f graph.yaml [--namespace ns]")
	}
	fs := flag.NewFlagSet("render", flag.ContinueOnError)
	f := fs.String("f", "", "graph file")
	ns := fs.String("namespace", "weft", "namespace")
	_ = fs.Parse(args[1:])
	_, g, err := readGraph(*f)
	if err != nil {
		return err
	}
	graph.Normalize(g)
	images := map[string]domain.Node{}
	for _, n := range g.Spec.Nodes {
		if n.Runtime.Kind == domain.RuntimeExternal && n.Runtime.Image != "" {
			images[n.ID] = n
		}
	}
	if len(images) == 0 {
		return fmt.Errorf("no external nodes with runtime.image in %s", *f)
	}
	ids := make([]string, 0, len(images))
	for id := range images {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		n := images[id]
		name := "weft-rt-" + g.Metadata.ID + "-" + id
		cpu, mem := n.Isolation.CPU, n.Isolation.Memory
		if cpu == "" {
			cpu = "500m"
		}
		if mem == "" {
			mem = "512Mi"
		}
		fmt.Printf(`---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: %[1]s
  namespace: %[2]s
  labels: {app.kubernetes.io/part-of: weft, weft.io/graph: %[3]s, weft.io/node: %[4]s}
spec:
  replicas: 1
  selector: {matchLabels: {app: %[1]s}}
  template:
    metadata: {labels: {app: %[1]s, app.kubernetes.io/part-of: weft}}
    spec:
      automountServiceAccountToken: false
      securityContext: {runAsNonRoot: true, runAsUser: 10001, seccompProfile: {type: RuntimeDefault}}
      containers:
        - name: agent
          image: %[5]s
          ports: [{containerPort: 8000}]
          env:
            - {name: PORT, value: "8000"}
            - {name: WEFT_RUNTIME_SECRET, valueFrom: {secretKeyRef: {name: weft-runtime, key: secret, optional: true}}}
          readinessProbe: {httpGet: {path: /v1/health, port: 8000}}
          resources: {requests: {cpu: %[6]s, memory: %[7]s}, limits: {memory: %[7]s}}
          securityContext: {allowPrivilegeEscalation: false, readOnlyRootFilesystem: true, capabilities: {drop: [ALL]}}
---
apiVersion: v1
kind: Service
metadata: {name: %[1]s, namespace: %[2]s}
spec:
  selector: {app: %[1]s}
  ports: [{port: 8000, targetPort: 8000}]
# set nodes[%[4]s].runtime.endpoint: http://%[1]s.%[2]s.svc:8000
`, name, *ns, g.Metadata.ID, id, n.Runtime.Image, cpu, mem)
	}
	return nil
}
