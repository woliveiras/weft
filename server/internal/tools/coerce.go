package tools

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Argument coercion (NAT-08). Small models often send the right value in the
// wrong JSON type: "false" for a boolean, "30" for an integer, a command line
// as one string where an argv array is expected, or ["main.go"] for a path.
// Before a tool runs, the gateway converts such values to the type the
// tool's schema declares when the conversion is unambiguous. Anything else
// is left alone and fails in the tool with a readable error.

type propSchema struct {
	Type       string                `json:"type"`
	Properties map[string]propSchema `json:"properties"`
	Items      *propSchema           `json:"items"`
}

// CoerceArgs converts top-level (and nested object) argument values to the
// types declared by schema. It returns the arguments (unchanged when nothing
// was converted) and one note per conversion, e.g. `replaceAll: "false" → false`.
func CoerceArgs(schema, args json.RawMessage) (json.RawMessage, []string) {
	var s propSchema
	if len(schema) == 0 || json.Unmarshal(schema, &s) != nil || len(s.Properties) == 0 {
		return args, nil
	}
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(string(args)))
	dec.UseNumber()
	if dec.Decode(&obj) != nil || obj == nil {
		return args, nil
	}
	var notes []string
	coerceObject(obj, s, "", &notes)
	if len(notes) == 0 {
		return args, nil
	}
	out, err := json.Marshal(obj)
	if err != nil {
		return args, nil
	}
	return out, notes
}

func coerceObject(obj map[string]any, s propSchema, prefix string, notes *[]string) {
	for k, v := range obj {
		ps, ok := s.Properties[k]
		if !ok {
			continue
		}
		if nv, ok := coerceValue(v, ps, prefix+k, notes); ok {
			obj[k] = nv
		}
	}
}

// coerceValue returns the converted value and true when it changed.
func coerceValue(v any, s propSchema, path string, notes *[]string) (any, bool) {
	note := func(to any) {
		from, _ := json.Marshal(v)
		toJSON, _ := json.Marshal(to)
		*notes = append(*notes, fmt.Sprintf("%s: %s → %s", path, truncateRunes(string(from), 60), truncateRunes(string(toJSON), 60)))
	}
	switch s.Type {
	case "boolean":
		if str, ok := v.(string); ok {
			switch strings.ToLower(strings.TrimSpace(str)) {
			case "true":
				note(true)
				return true, true
			case "false":
				note(false)
				return false, true
			}
		}
	case "integer", "number":
		if str, ok := v.(string); ok {
			str = strings.TrimSpace(str)
			if s.Type == "integer" {
				if n, err := strconv.ParseInt(str, 10, 64); err == nil {
					note(n)
					return n, true
				}
			} else if f, err := strconv.ParseFloat(str, 64); err == nil {
				note(f)
				return f, true
			}
		}
	case "string":
		switch x := v.(type) {
		case []any:
			if len(x) == 1 {
				if str, ok := x[0].(string); ok {
					note(str)
					return str, true
				}
			}
		case json.Number:
			note(x.String())
			return x.String(), true
		case bool:
			note(strconv.FormatBool(x))
			return strconv.FormatBool(x), true
		}
	case "array":
		str, ok := v.(string)
		if !ok {
			if arr, isArr := v.([]any); isArr && s.Items != nil {
				changed := false
				for i, e := range arr {
					if nv, ok := coerceValue(e, *s.Items, fmt.Sprintf("%s[%d]", path, i), notes); ok {
						arr[i], changed = nv, true
					}
				}
				return arr, changed
			}
			return nil, false
		}
		trimmed := strings.TrimSpace(str)
		if strings.HasPrefix(trimmed, "[") {
			var arr []any
			if json.Unmarshal([]byte(trimmed), &arr) == nil {
				note(arr)
				return arr, true
			}
		}
		if s.Items != nil && s.Items.Type == "string" {
			if words, err := SplitCommand(trimmed); err == nil && len(words) > 0 {
				arr := make([]any, len(words))
				for i, w := range words {
					arr[i] = w
				}
				note(arr)
				return arr, true
			}
		}
	case "object":
		switch x := v.(type) {
		case string:
			var obj map[string]any
			if json.Unmarshal([]byte(strings.TrimSpace(x)), &obj) == nil && obj != nil {
				coerceObject(obj, s, path+".", notes)
				note(obj)
				return obj, true
			}
		case map[string]any:
			before := len(*notes)
			coerceObject(x, s, path+".", notes)
			return x, len(*notes) > before
		}
	}
	return nil, false
}

// argError turns a JSON decoding error into a message a model can act on.
func argError(err error) error {
	var te *json.UnmarshalTypeError
	if errors.As(err, &te) {
		field := te.Field
		if i := strings.LastIndex(field, "."); i >= 0 {
			field = field[i+1:]
		}
		if field == "" {
			field = "the arguments"
		}
		return fmt.Errorf("invalid arguments: %s must be %s, got a JSON %s", field, describeGoType(te.Type.String()), te.Value)
	}
	var se *json.SyntaxError
	if errors.As(err, &se) {
		return fmt.Errorf("invalid arguments: not valid JSON (%v)", err)
	}
	return fmt.Errorf("invalid arguments: %w", err)
}

func describeGoType(t string) string {
	switch {
	case t == "bool":
		return "a boolean (true or false, without quotes)"
	case strings.HasPrefix(t, "int") || strings.HasPrefix(t, "uint"):
		return "an integer (without quotes)"
	case strings.HasPrefix(t, "float"):
		return "a number (without quotes)"
	case t == "string":
		return "a string"
	case strings.HasPrefix(t, "[]string"):
		return `an array of strings, e.g. ["go","test","./..."]`
	case strings.HasPrefix(t, "[]"):
		return "an array"
	case strings.HasPrefix(t, "map[") || strings.HasPrefix(t, "struct"):
		return "an object"
	default:
		return "a " + t
	}
}
