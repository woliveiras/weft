package trigger

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/woliveiras/weft/server/internal/tools"
)

// jiraSearchFormatError reports only locally defined shape categories. Provider
// values, arbitrary keys, issue content and JSON decoder errors must not escape.
func jiraSearchFormatError(res *tools.Result, payload json.RawMessage, stage string) error {
	raw := res.Data
	if len(raw) == 0 {
		raw = json.RawMessage(res.Content)
	}
	return fmt.Errorf("unexpected Jira issue search result (stage=%s; envelope=%s; payload=%s)", stage, jiraSearchShape(raw), jiraSearchShape(payload))
}

func jiraSearchJSONType(raw json.RawMessage) string {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "missing"
	}
	if !json.Valid(raw) {
		return "invalid_json"
	}
	switch raw[0] {
	case '{':
		return "object"
	case '[':
		return "array"
	case '"':
		return "string"
	case 'n':
		return "null"
	case 't', 'f':
		return "boolean"
	default:
		return "number"
	}
}

func jiraSearchShape(raw json.RawMessage) string {
	kind := jiraSearchJSONType(raw)
	if kind != "object" {
		return kind
	}
	var object map[string]json.RawMessage
	if json.Unmarshal(raw, &object) != nil {
		return "invalid_json"
	}
	parts := []string{"object"}
	for _, key := range []string{"issues", "results", "data", "structuredContent", "content", "nextPageToken", "isError", "error"} {
		parts = append(parts, key+"="+jiraSearchJSONType(object[key]))
	}
	if jiraSearchJSONType(object["content"]) == "array" {
		var blocks []json.RawMessage
		if json.Unmarshal(object["content"], &blocks) == nil {
			count := strconv.Itoa(min(len(blocks), 10))
			if len(blocks) > 10 {
				count += "+"
			}
			parts = append(parts, "contentCount="+count)
			for index, block := range blocks[:min(len(blocks), 3)] {
				category := jiraSearchJSONType(block)
				if category == "object" {
					var fields map[string]json.RawMessage
					if json.Unmarshal(block, &fields) == nil {
						var blockType string
						if json.Unmarshal(fields["type"], &blockType) == nil {
							switch blockType {
							case "text", "image", "audio", "resource", "resource_link":
								category = blockType
							default:
								category = "other"
							}
						}
					}
				}
				parts = append(parts, "block"+strconv.Itoa(index)+"="+category)
			}
		}
	}
	return strings.Join(parts, ",")
}
