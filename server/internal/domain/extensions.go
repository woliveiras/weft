package domain

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ExtensionPrefix marks document keys Weft does not interpret but keeps.
// Tools that annotate graphs (editors, generators, review bots) put their
// data under "x-..." keys on the graph, its metadata, a node or a channel,
// and every save round-trips them verbatim. Any other unknown key is
// rejected at the API boundary rather than silently dropped.
const ExtensionPrefix = "x-"

// Extensions holds the "x-" keys of one document object.
type Extensions map[string]any

// IsExtensionKey reports whether a document key is an extension key.
func IsExtensionKey(k string) bool {
	return strings.HasPrefix(k, ExtensionPrefix) && len(k) > len(ExtensionPrefix)
}

func (e Extensions) keys() []string {
	ks := make([]string, 0, len(e))
	for k := range e {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

// marshalJSONExt encodes plain (an object) and appends the extension keys.
func marshalJSONExt(plain any, ext Extensions) ([]byte, error) {
	b, err := json.Marshal(plain)
	if err != nil || len(ext) == 0 {
		return b, err
	}
	if len(b) < 2 || b[0] != '{' || b[len(b)-1] != '}' {
		return nil, fmt.Errorf("extensions: %T does not encode as an object", plain)
	}
	var buf bytes.Buffer
	buf.Write(b[:len(b)-1])
	for _, k := range ext.keys() {
		v, err := json.Marshal(ext[k])
		if err != nil {
			return nil, fmt.Errorf("extension %q: %w", k, err)
		}
		if buf.Len() > 1 {
			buf.WriteByte(',')
		}
		kb, _ := json.Marshal(k)
		buf.Write(kb)
		buf.WriteByte(':')
		buf.Write(v)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// extensionsFromJSON collects the extension keys of a JSON object.
func extensionsFromJSON(b []byte) (Extensions, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return nil, err
	}
	var ext Extensions
	for k, v := range raw {
		if !IsExtensionKey(k) {
			continue
		}
		var val any
		if err := json.Unmarshal(v, &val); err != nil {
			return nil, fmt.Errorf("extension %q: %w", k, err)
		}
		if ext == nil {
			ext = Extensions{}
		}
		ext[k] = val
	}
	return ext, nil
}

// marshalYAMLExt encodes plain as a mapping and appends the extension keys.
func marshalYAMLExt(plain any, ext Extensions) (any, error) {
	if len(ext) == 0 {
		return plain, nil
	}
	var n yaml.Node
	if err := n.Encode(plain); err != nil {
		return nil, err
	}
	if n.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("extensions: %T does not encode as a mapping", plain)
	}
	for _, k := range ext.keys() {
		var key, val yaml.Node
		key.SetString(k)
		if err := val.Encode(ext[k]); err != nil {
			return nil, fmt.Errorf("extension %q: %w", k, err)
		}
		n.Content = append(n.Content, &key, &val)
	}
	return &n, nil
}

// extensionsFromYAML collects the extension keys of a YAML mapping. Values
// are normalised to JSON-compatible types, because documents are stored as
// JSON.
func extensionsFromYAML(n *yaml.Node) (Extensions, error) {
	if n.Kind != yaml.MappingNode {
		return nil, nil
	}
	var ext Extensions
	for i := 0; i+1 < len(n.Content); i += 2 {
		k := n.Content[i].Value
		if !IsExtensionKey(k) {
			continue
		}
		var val any
		if err := n.Content[i+1].Decode(&val); err != nil {
			return nil, fmt.Errorf("extension %q: %w", k, err)
		}
		val, err := jsonCompatible(val)
		if err != nil {
			return nil, fmt.Errorf("extension %q: %w", k, err)
		}
		if ext == nil {
			ext = Extensions{}
		}
		ext[k] = val
	}
	return ext, nil
}

// jsonCompatible rejects YAML values JSON cannot represent (non-string map
// keys), so a document that decodes also stores.
func jsonCompatible(v any) (any, error) {
	switch t := v.(type) {
	case map[string]any:
		for k, x := range t {
			y, err := jsonCompatible(x)
			if err != nil {
				return nil, err
			}
			t[k] = y
		}
		return t, nil
	case map[any]any:
		out := make(map[string]any, len(t))
		for k, x := range t {
			ks, ok := k.(string)
			if !ok {
				return nil, fmt.Errorf("map key %v is not a string", k)
			}
			y, err := jsonCompatible(x)
			if err != nil {
				return nil, err
			}
			out[ks] = y
		}
		return out, nil
	case []any:
		for i, x := range t {
			y, err := jsonCompatible(x)
			if err != nil {
				return nil, err
			}
			t[i] = y
		}
		return t, nil
	default:
		return v, nil
	}
}

// The plain types share their fields with the real ones but none of the
// methods below, so encoding them does not recurse.
type (
	graphPlain    Graph
	metadataPlain GraphMetadata
	nodePlain     Node
	channelPlain  Channel
)

func (g Graph) MarshalJSON() ([]byte, error) { return marshalJSONExt(graphPlain(g), g.Extensions) }
func (g Graph) MarshalYAML() (any, error)    { return marshalYAMLExt(graphPlain(g), g.Extensions) }

func (g *Graph) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, (*graphPlain)(g)); err != nil {
		return err
	}
	ext, err := extensionsFromJSON(b)
	g.Extensions = ext
	return err
}

func (g *Graph) UnmarshalYAML(n *yaml.Node) error {
	if err := n.Decode((*graphPlain)(g)); err != nil {
		return err
	}
	ext, err := extensionsFromYAML(n)
	g.Extensions = ext
	return err
}

func (m GraphMetadata) MarshalJSON() ([]byte, error) {
	return marshalJSONExt(metadataPlain(m), m.Extensions)
}
func (m GraphMetadata) MarshalYAML() (any, error) {
	return marshalYAMLExt(metadataPlain(m), m.Extensions)
}

func (m *GraphMetadata) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, (*metadataPlain)(m)); err != nil {
		return err
	}
	ext, err := extensionsFromJSON(b)
	m.Extensions = ext
	return err
}

func (m *GraphMetadata) UnmarshalYAML(n *yaml.Node) error {
	if err := n.Decode((*metadataPlain)(m)); err != nil {
		return err
	}
	ext, err := extensionsFromYAML(n)
	m.Extensions = ext
	return err
}

func (nd Node) MarshalJSON() ([]byte, error) { return marshalJSONExt(nodePlain(nd), nd.Extensions) }
func (nd Node) MarshalYAML() (any, error)    { return marshalYAMLExt(nodePlain(nd), nd.Extensions) }

func (nd *Node) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, (*nodePlain)(nd)); err != nil {
		return err
	}
	ext, err := extensionsFromJSON(b)
	nd.Extensions = ext
	return err
}

func (nd *Node) UnmarshalYAML(n *yaml.Node) error {
	if err := n.Decode((*nodePlain)(nd)); err != nil {
		return err
	}
	ext, err := extensionsFromYAML(n)
	nd.Extensions = ext
	return err
}

func (c Channel) MarshalJSON() ([]byte, error) { return marshalJSONExt(channelPlain(c), c.Extensions) }
func (c Channel) MarshalYAML() (any, error)    { return marshalYAMLExt(channelPlain(c), c.Extensions) }

func (c *Channel) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, (*channelPlain)(c)); err != nil {
		return err
	}
	ext, err := extensionsFromJSON(b)
	c.Extensions = ext
	return err
}

func (c *Channel) UnmarshalYAML(n *yaml.Node) error {
	if err := n.Decode((*channelPlain)(c)); err != nil {
		return err
	}
	ext, err := extensionsFromYAML(n)
	c.Extensions = ext
	return err
}
