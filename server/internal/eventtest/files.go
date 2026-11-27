package eventtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidateCaptures checks the exported wire data directly so that an unknown
// field cannot disappear during decoding into a domain.Event.
func ValidateCaptures(dir string) (events, kinds, scenarios int, err error) {
	paths, err := fs.Glob(os.DirFS(dir), "events-*.json")
	if err != nil {
		return 0, 0, 0, err
	}
	if len(paths) == 0 {
		return 0, 0, 0, fmt.Errorf("no events-*.json captures in %s", dir)
	}
	schema, err := eventSchema()
	if err != nil {
		return 0, 0, 0, err
	}
	types := map[string]struct{}{}
	var invalid []error
	for _, name := range paths {
		raw, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			return 0, 0, 0, err
		}
		var capture struct {
			Test   string
			Events []json.RawMessage
		}
		if err := json.Unmarshal(raw, &capture); err != nil {
			return 0, 0, 0, fmt.Errorf("%s: %w", name, err)
		}
		if capture.Test == "" || capture.Events == nil {
			return 0, 0, 0, fmt.Errorf("%s: missing test or events", name)
		}
		for i, raw := range capture.Events {
			wire, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
			if err == nil {
				err = schema.Validate(wire)
			}
			if err != nil {
				invalid = append(invalid, fmt.Errorf("%s (%s), event %d: %w", capture.Test, name, i+1, err))
				continue
			}
			types[wire.(map[string]any)["type"].(string)] = struct{}{}
			events++
		}
	}
	return events, len(types), len(paths), errors.Join(invalid...)
}
