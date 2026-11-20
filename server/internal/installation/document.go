// Package installation defines the secret-free declarative installation wire format.
package installation

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/woliveiras/weft/server/internal/graph"
	"gopkg.in/yaml.v3"
)

const MaxBytes = 4 << 20

var Kinds = []string{"InferencePlugin", "ModelProvider", "PriceCatalog", "CostPolicy", "Budget", "NetworkPolicy", "MCPConnection", "SCMConnection", "IssueConnection", "Project", "Graph", "IssueTrigger"}

type Document struct {
	Extensions map[string]any `json:"-" yaml:"-"`
	APIVersion string         `json:"apiVersion" yaml:"apiVersion"`
	Kind       string         `json:"kind" yaml:"kind"`
	Metadata   map[string]any `json:"metadata" yaml:"metadata"`
	Spec       map[string]any `json:"spec" yaml:"spec"`
}

func (d Document) Name() string {
	v, _ := d.Metadata["name"].(string)
	if d.Kind == "Graph" {
		if id, _ := d.Metadata["id"].(string); id != "" {
			return id
		}
	}
	return v
}
func (d Document) Key() string      { return d.Kind + "/" + d.Name() }
func (d Document) Revision() string { v, _ := d.Metadata["revision"].(string); return v }
func New(kind, name string, spec any) Document {
	return Document{APIVersion: "weft.io/v1", Kind: kind, Metadata: map[string]any{"name": name}, Spec: Map(spec)}
}
func Map(v any) map[string]any {
	b, _ := json.Marshal(v)
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	return m
}
func Clone(d Document) Document {
	b, _ := json.Marshal(d)
	var out Document
	_ = json.Unmarshal(b, &out)
	return out
}
func Digest(v any) string {
	b, _ := json.Marshal(v)
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:])
}
func Rank(kind string) int {
	for i, k := range Kinds {
		if k == kind {
			return i
		}
	}
	return len(Kinds)
}
func Sort(ds []Document) {
	sort.Slice(ds, func(i, j int) bool {
		if ds[i].Kind != ds[j].Kind {
			return Rank(ds[i].Kind) < Rank(ds[j].Kind)
		}
		if ds[i].Kind == "ModelProvider" {
			a, _ := ds[i].Spec["default"].(bool)
			b, _ := ds[j].Spec["default"].(bool)
			if a != b {
				return !a
			}
		}
		return ds[i].Name() < ds[j].Name()
	})
}

// Parse rejects aliases and duplicate keys before decoding and never quotes source text.
func Parse(files []string) ([]Document, error) {
	var out []Document
	seen := map[string]bool{}
	total := 0
	for fi, raw := range files {
		total += len(raw)
		if total > MaxBytes {
			return nil, fmt.Errorf("documents: exceeds 4 MiB")
		}
		dec := yaml.NewDecoder(strings.NewReader(raw))
		for di := 0; ; di++ {
			var node yaml.Node
			err := dec.Decode(&node)
			if err == io.EOF {
				break
			}
			if err != nil {
				return nil, fmt.Errorf("files[%d].documents[%d]: invalid YAML", fi, di)
			}
			if len(node.Content) == 0 {
				continue
			}
			if node.Content[0].Tag == "!!null" {
				continue
			}
			path := fmt.Sprintf("files[%d].documents[%d]", fi, di)
			if err = checkNode(&node, path); err != nil {
				return nil, err
			}
			var m map[string]any
			if node.Decode(&m) != nil {
				return nil, fmt.Errorf("%s: expected an object", path)
			}
			extensions := map[string]any{}
			if m["kind"] == "Graph" {
				for k, v := range m {
					if strings.HasPrefix(k, "x-") {
						extensions[k] = v
						delete(m, k)
					}
				}
			}
			var d Document
			if err = Decode(m, &d, path); err != nil {
				return nil, err
			}
			d.Extensions = extensions
			if d.APIVersion != "weft.io/v1" {
				return nil, fmt.Errorf("%s.apiVersion: expected weft.io/v1", path)
			}
			if Rank(d.Kind) == len(Kinds) {
				return nil, fmt.Errorf("%s.kind: unsupported kind", path)
			}
			if d.Kind == "Graph" && d.Name() != "" {
				if id, _ := d.Metadata["id"].(string); id == "" {
					d.Metadata["id"] = graph.Slug(d.Name())
				}
			}
			if d.Kind == "Graph" && d.Name() == "" {
				return nil, fmt.Errorf("%s.metadata.name: required", path)
			}
			if strings.TrimSpace(d.Name()) == "" || len(d.Name()) > 200 || strings.ContainsAny(d.Name(), "\x00\r\n") {
				return nil, fmt.Errorf("%s.metadata.name: invalid identity", path)
			}
			if d.Spec == nil {
				return nil, fmt.Errorf("%s.spec: required object", path)
			}
			for _, field := range []string{"apiKey", "credential", "token", "oauthClientSecret", "processEnvironment", "authentication"} {
				if _, ok := d.Spec[field]; ok {
					return nil, fmt.Errorf("%s.spec.%s: plaintext credential input forbidden; use secret references", path, field)
				}
			}
			if d.Kind != "Graph" {
				for k := range d.Metadata {
					if k != "name" && k != "revision" {
						return nil, fmt.Errorf("%s.metadata: unknown field", path)
					}
				}
			}
			if v, ok := d.Metadata["revision"]; ok {
				if _, ok = v.(string); !ok {
					return nil, fmt.Errorf("%s.metadata.revision: expected string", path)
				}
			}
			if seen[d.Key()] {
				return nil, fmt.Errorf("%s.metadata.name: duplicate object", path)
			}
			seen[d.Key()] = true
			out = append(out, d)
			if len(out) > 500 {
				return nil, fmt.Errorf("documents: at most 500 objects")
			}
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("documents: no YAML objects")
	}
	Sort(out)
	return out, nil
}
func checkNode(n *yaml.Node, path string) error {
	if n.Kind == yaml.AliasNode {
		return fmt.Errorf("%s: YAML aliases are not supported", path)
	}
	if n.Kind == yaml.MappingNode {
		seen := map[string]bool{}
		for i := 0; i < len(n.Content); i += 2 {
			k := n.Content[i]
			if k.Tag != "!!str" || seen[k.Value] {
				return fmt.Errorf("%s: duplicate or non-string key", path)
			}
			seen[k.Value] = true
		}
	}
	for _, c := range n.Content {
		if err := checkNode(c, path); err != nil {
			return err
		}
	}
	return nil
}

// Decode checks JSON field names recursively, including embedded API input structs.
func Decode(v any, out any, path string) error {
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("%s: invalid object", path)
	}
	var raw any
	_ = json.Unmarshal(b, &raw)
	if err = checkFields(raw, reflect.TypeOf(out).Elem(), path); err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	if err = dec.Decode(out); err != nil {
		var te *json.UnmarshalTypeError
		if e, ok := err.(*json.UnmarshalTypeError); ok {
			te = e
		}
		if te != nil && te.Field != "" {
			return fmt.Errorf("%s.%s: invalid type", path, te.Field)
		}
		return fmt.Errorf("%s: invalid fields or types", path)
	}
	return nil
}
func fields(t reflect.Type, m map[string]reflect.Type) {
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.PkgPath != "" {
			continue
		}
		if f.Anonymous {
			ft := f.Type
			for ft.Kind() == reflect.Pointer {
				ft = ft.Elem()
			}
			if ft.Kind() == reflect.Struct {
				fields(ft, m)
				continue
			}
		}
		name := strings.Split(f.Tag.Get("json"), ",")[0]
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		m[name] = f.Type
	}
}
func checkFields(v any, t reflect.Type, path string) error {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.Kind() == reflect.Struct {
		if m, ok := v.(map[string]any); ok {
			fs := map[string]reflect.Type{}
			fields(t, fs)
			for k, x := range m {
				ft, ok := fs[k]
				if !ok {
					return fmt.Errorf("%s.%s: unknown field", path, safeField(k))
				}
				if err := checkFields(x, ft, path+"."+k); err != nil {
					return err
				}
			}
		}
	}
	if t.Kind() == reflect.Slice {
		if xs, ok := v.([]any); ok {
			for i, x := range xs {
				if err := checkFields(x, t.Elem(), fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
func Export(ds []Document) (string, error) {
	Sort(ds)
	var b bytes.Buffer
	b.WriteString("# Secret references preserve stored credentials. Replace preserve: true with\n# env: WEFT_SECRET_NAME or file: relative.key to install or rotate a credential.\n")
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	for _, d := range ds {
		if err := enc.Encode(d); err != nil {
			return "", err
		}
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return b.String(), nil
}

type Request struct {
	Files         []string          `json:"files"`
	Revisions     map[string]string `json:"revisions,omitempty"`
	Expected      map[string]string `json:"expected,omitempty"`
	Force         []string          `json:"force,omitempty"`
	RecordConsent bool              `json:"recordConsent,omitempty"`
	Confirmed     bool              `json:"confirmed,omitempty"`
}
type Change struct {
	Path   string `json:"path"`
	Before any    `json:"before,omitempty"`
	After  any    `json:"after,omitempty"`
}
type ObjectPlan struct {
	Key      string   `json:"key"`
	Action   string   `json:"action"`
	Revision string   `json:"revision"`
	Changes  []Change `json:"changes"`
	Secret   string   `json:"secret,omitempty"`
	Error    string   `json:"error,omitempty"`
	Status   string   `json:"status,omitempty"`
	Consent  string   `json:"consent,omitempty"`
	Warning  string   `json:"warning,omitempty"`
	Result   string   `json:"result,omitempty"`
}
type Plan struct {
	Objects []ObjectPlan `json:"objects"`
	OK      bool         `json:"ok"`
}

func Diff(before, after map[string]any) []Change {
	out := []Change{}
	diff(before, after, "spec", &out)
	return out
}
func diff(before, after any, path string, out *[]Change) {
	if reflect.DeepEqual(before, after) {
		return
	}
	b, bok := before.(map[string]any)
	a, aok := after.(map[string]any)
	if bok && aok {
		keys := map[string]bool{}
		for k := range b {
			keys[k] = true
		}
		for k := range a {
			keys[k] = true
		}
		ordered := []string{}
		for k := range keys {
			ordered = append(ordered, k)
		}
		sort.Strings(ordered)
		for _, k := range ordered {
			diff(b[k], a[k], path+"."+k, out)
		}
		return
	}
	*out = append(*out, Change{path, before, after})
}

func safeField(s string) string {
	if regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]{0,63}$`).MatchString(s) {
		return s
	}
	return "<unknown>"
}

// MarshalJSON and MarshalYAML preserve the existing graph extension contract.
func (d Document) MarshalJSON() ([]byte, error) {
	type plain Document
	b, err := json.Marshal(plain(d))
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err = json.Unmarshal(b, &m); err != nil {
		return nil, err
	}
	for k, v := range d.Extensions {
		m[k] = v
	}
	return json.Marshal(m)
}
func (d *Document) UnmarshalJSON(b []byte) error {
	type plain Document
	var v plain
	if err := json.Unmarshal(b, &v); err != nil {
		return err
	}
	*d = Document(v)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return err
	}
	d.Extensions = map[string]any{}
	for k, v := range m {
		if strings.HasPrefix(k, "x-") {
			d.Extensions[k] = v
		}
	}
	return nil
}
func (d Document) MarshalYAML() (any, error) { return Map(d), nil }
