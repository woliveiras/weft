package graph

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/woliveiras/weft/server/internal/domain"
)

var (
	graphType      = reflect.TypeOf(domain.Graph{})
	extensionsType = reflect.TypeOf(domain.Extensions(nil))
)

// UnknownFields lists keys of a decoded graph document (the generic value
// produced by decoding JSON or YAML into `any`) that the graph schema does
// not define. Decoding into domain.Graph drops such keys, so an editor or
// a typo ("promt") would otherwise lose data without notice. "x-" keys are
// accepted where the schema keeps them (graph, metadata, nodes, channels);
// free-form maps such as node config are not inspected.
//
// Values of the wrong shape are left to the typed decoder: this only
// reports keys.
func UnknownFields(doc any) []Issue {
	var out []Issue
	checkFields("", doc, graphType, &out)
	return out
}

func checkFields(path string, raw any, t reflect.Type, out *[]Issue) {
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		m, ok := raw.(map[string]any)
		if !ok {
			return
		}
		fields, extensible := schemaFields(t)
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			p := joinPath(path, k)
			if ft, ok := fields[k]; ok {
				checkFields(p, m[k], ft, out)
				continue
			}
			if domain.IsExtensionKey(k) {
				if !extensible {
					*out = append(*out, Issue{p, "extension keys are only kept on the graph, metadata, nodes and channels"})
				}
				continue
			}
			*out = append(*out, Issue{p, fmt.Sprintf("unknown field %q", k)})
		}
	case reflect.Slice, reflect.Array:
		s, ok := raw.([]any)
		if !ok {
			return
		}
		for i, v := range s {
			checkFields(fmt.Sprintf("%s[%d]", path, i), v, t.Elem(), out)
		}
	case reflect.Map:
		m, ok := raw.(map[string]any)
		if !ok || t.Elem().Kind() == reflect.Interface {
			return
		}
		for k, v := range m {
			checkFields(joinPath(path, k), v, t.Elem(), out)
		}
	}
}

// schemaFields maps document keys (json tags) to field types, and reports
// whether the struct keeps extension keys.
func schemaFields(t reflect.Type) (map[string]reflect.Type, bool) {
	fields := map[string]reflect.Type{}
	extensible := false
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		if f.Type == extensionsType {
			extensible = true
			continue
		}
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		fields[name] = f.Type
	}
	return fields, extensible
}

func joinPath(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

// Decode parses a graph document from YAML or JSON and lists the keys the
// schema does not define (see UnknownFields). The error is a syntax or type
// error in the document; unknown keys are returned as issues, so callers
// can report them alongside validation findings.
func Decode(b []byte, isYAML bool) (*domain.Graph, []Issue, error) {
	var g domain.Graph
	var doc any
	if isYAML {
		if err := yaml.Unmarshal(b, &g); err != nil {
			return nil, nil, err
		}
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return nil, nil, err
		}
	} else {
		if err := json.Unmarshal(b, &g); err != nil {
			return nil, nil, err
		}
		if err := json.Unmarshal(b, &doc); err != nil {
			return nil, nil, err
		}
	}
	return &g, UnknownFields(doc), nil
}
