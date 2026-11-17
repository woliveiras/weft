package mcpconfig

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
)

// ArgumentRule constrains either one top-level string or a complete string tuple.
// Rules for one tool are ANDed. Once configured, unlisted tools are excluded.
type ArgumentRule struct {
	Tool         string     `json:"tool"`
	Argument     string     `json:"argument,omitempty"`
	Values       []string   `json:"values,omitempty"`
	Arguments    []string   `json:"arguments,omitempty"`
	Combinations [][]string `json:"combinations,omitempty"`
}

func validateArgumentPolicy(rules []ArgumentRule) error {
	if len(rules) > 64 {
		return fmt.Errorf("%w: at most 64 argument rules", ErrInvalid)
	}
	seen := map[string]bool{}
	for _, r := range rules {
		if len(r.Arguments) > 0 || len(r.Combinations) > 0 {
			if err := validateCombinations(r); err != nil {
				return err
			}
			key := r.Tool + "/@combinations"
			if seen[key] {
				return fmt.Errorf("%w: duplicate combination rule", ErrInvalid)
			}
			seen[key] = true
			continue
		}
		key := r.Tool + "/" + r.Argument
		if !toolName.MatchString(r.Tool) || !toolName.MatchString(r.Argument) || seen[key] || len(r.Values) < 1 || len(r.Values) > 32 {
			return fmt.Errorf("%w: argument rules require unique tool/argument names and 1–32 values", ErrInvalid)
		}
		seen[key] = true
		values := map[string]bool{}
		for _, v := range r.Values {
			if v == "" || len(v) > 256 || strings.TrimSpace(v) != v || strings.ContainsFunc(v, unicode.IsControl) || values[v] {
				return fmt.Errorf("%w: argument values must be distinct non-empty strings of at most 256 bytes without surrounding whitespace or controls", ErrInvalid)
			}
			values[v] = true
		}
	}
	return nil
}
func argumentPolicyTool(rules []ArgumentRule, tool Tool) bool {
	if len(rules) == 0 {
		return true
	}
	var schema struct {
		Properties map[string]struct {
			Type string `json:"type"`
		} `json:"properties"`
	}
	if json.Unmarshal(tool.InputSchema, &schema) != nil {
		return false
	}
	found := false
	for _, r := range rules {
		if r.Tool == tool.Name {
			found = true
			fields := r.Arguments
			if len(fields) == 0 {
				fields = []string{r.Argument}
			}
			for _, field := range fields {
				if schema.Properties[field].Type != "string" {
					return false
				}
			}
		}
	}
	return found
}
func argumentPolicyAllows(rules []ArgumentRule, tool Tool, args map[string]json.RawMessage) bool {
	if !argumentPolicyTool(rules, tool) {
		return false
	}
	for _, r := range rules {
		if r.Tool != tool.Name {
			continue
		}
		if len(r.Arguments) > 0 {
			matched := false
			for _, combination := range r.Combinations {
				if len(combination) != len(r.Arguments) {
					return false
				}
				all := true
				for i, field := range r.Arguments {
					var value string
					if json.Unmarshal(args[field], &value) != nil || value != combination[i] {
						all = false
						break
					}
				}
				if all {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
			continue
		}
		var value string
		if json.Unmarshal(args[r.Argument], &value) != nil {
			return false
		}
		matched := false
		for _, allowed := range r.Values {
			if value == allowed {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

// Combinations constrain a whole tuple; independently allowed values must never
// turn into a Cartesian product of resources from different reviewed tuples.
func validateCombinations(r ArgumentRule) error {
	if !toolName.MatchString(r.Tool) || r.Argument != "" || len(r.Values) != 0 || len(r.Arguments) < 2 || len(r.Arguments) > 8 || len(r.Combinations) < 1 || len(r.Combinations) > 32 {
		return fmt.Errorf("%w: combination rules require 2–8 arguments and 1–32 combinations, without independent values", ErrInvalid)
	}
	fields := map[string]bool{}
	for _, field := range r.Arguments {
		if !toolName.MatchString(field) || fields[field] {
			return fmt.Errorf("%w: combination argument names must be distinct", ErrInvalid)
		}
		fields[field] = true
	}
	seen := map[string]bool{}
	for _, combination := range r.Combinations {
		if len(combination) != len(r.Arguments) {
			return fmt.Errorf("%w: each combination must supply every argument", ErrInvalid)
		}
		for _, value := range combination {
			if value == "" || len(value) > 256 || strings.TrimSpace(value) != value || strings.ContainsFunc(value, unicode.IsControl) {
				return fmt.Errorf("%w: invalid combination value", ErrInvalid)
			}
		}
		key, err := json.Marshal(combination)
		if err != nil {
			return fmt.Errorf("%w: invalid combination", ErrInvalid)
		}
		if seen[string(key)] {
			return fmt.Errorf("%w: duplicate combination", ErrInvalid)
		}
		seen[string(key)] = true
	}
	return nil
}
