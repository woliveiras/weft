package localinstall

import (
	"context"
	_ "embed"
	"errors"
	"fmt"

	"github.com/woliveiras/weft/server/internal/app"
	"github.com/woliveiras/weft/server/internal/domain"
	"github.com/woliveiras/weft/server/internal/graph"
	"github.com/woliveiras/weft/server/internal/storage"
)

//go:embed software-factory.yaml
var factoryDocument []byte

//go:embed software-factory-v2.yaml
var factoryV2Document []byte

// factoryTemplate is a workflow seeded once on a fresh local install.
type factoryTemplate struct {
	id, seedKey string
	doc         []byte
}

// factoryTemplates: v1 (no shell, human-run tests) and v2 (NAT-10: role
// skills, edit and navigation tools, declared checks with check.run).
var factoryTemplates = []factoryTemplate{
	{id: "software-factory", seedKey: "local-install/software-factory-v1", doc: factoryDocument},
	{id: "software-factory-v2", seedKey: "local-install/software-factory-v2", doc: factoryV2Document},
}

func factoryGraph() (*domain.Graph, error) { return decodeFactory(factoryDocument) }

// FactoryV2Graph returns the Software Factory v2 workflow (validated), for
// seeding and for the evaluation suite.
func FactoryV2Graph() (*domain.Graph, error) { return decodeFactory(factoryV2Document) }

func decodeFactory(doc []byte) (*domain.Graph, error) {
	g, unknown, err := graph.Decode(doc, true)
	if err != nil {
		return nil, err
	}
	if len(unknown) > 0 {
		return nil, errors.New("native factory contains unknown fields")
	}
	graph.Normalize(g)
	report := graph.Validate(g, map[domain.RuntimeKind]bool{domain.RuntimeNative: true})
	if len(report.Errors) > 0 {
		return nil, fmt.Errorf("invalid native Software Factory %s: %v", g.Metadata.ID, report.Errors)
	}
	return g, nil
}

// Seed each template once, preserving existing workflows and intentional
// later deletion. A crash between graph creation and marker persistence is
// safe to retry.
func seedFactory(ctx context.Context, a *app.App) error {
	for _, t := range factoryTemplates {
		if err := seedTemplate(ctx, a, t); err != nil {
			return err
		}
	}
	return nil
}

func seedTemplate(ctx context.Context, a *app.App, t factoryTemplate) error {
	if _, err := a.Store.GetSetting(ctx, t.seedKey); err == nil {
		return nil
	} else if !errors.Is(err, storage.ErrNotFound) {
		return err
	}
	if _, err := a.Store.GetGraph(ctx, t.id, 0); errors.Is(err, storage.ErrNotFound) {
		g, e := decodeFactory(t.doc)
		if e != nil {
			return e
		}
		if _, _, e = a.Engine.SaveGraph(ctx, g, 0, "native-install"); e != nil {
			return e
		}
	} else if err != nil {
		return err
	}
	_, err := a.Store.PutSetting(ctx, t.seedKey, []byte(`{"version":1}`), 0, "native-install")
	return err
}
