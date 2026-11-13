package eventtest

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/woliveiras/weft/server/internal/domain"
)

var eventSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	const id = "https://weft.io/schemas/v1/event.schema.json"
	cwd, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	file, err := openEventSchema(cwd)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	doc, err := jsonschema.UnmarshalJSON(file)
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	compiler.AssertFormat()
	if err := compiler.AddResource(id, doc); err != nil {
		return nil, err
	}
	return compiler.Compile(id)
})

func openEventSchema(dir string) (*os.File, error) {
	for {
		file, err := os.Open(filepath.Join(dir, "schemas", "event.schema.json"))
		if err == nil || !errors.Is(err, os.ErrNotExist) {
			return file, err
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return nil, fmt.Errorf("find schemas/event.schema.json: %w", os.ErrNotExist)
		}
		dir = parent
	}
}

func validateEvent(schema *jsonschema.Schema, event *domain.Event) error {
	raw, err := json.Marshal(event)
	if err != nil {
		return err
	}
	wire, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return err
	}
	return schema.Validate(wire)
}
