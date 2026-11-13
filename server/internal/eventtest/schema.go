package eventtest

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/woliveiras/weft/server/internal/domain"
)

var eventSchema = sync.OnceValues(func() (*jsonschema.Schema, error) {
	const id = "https://weft.io/schemas/v1/event.schema.json"
	file, err := os.Open(filepath.Join("..", "..", "..", "schemas", "event.schema.json"))
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
