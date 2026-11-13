package domain_test

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestEmittedEventTypesAreInSchemaAndCatalog(t *testing.T) {
	root := filepath.Join("..", "..", "..")
	raw, err := os.ReadFile(filepath.Join(root, "schemas", "event.schema.json"))
	if err != nil {
		t.Fatal(err)
	}
	var schema struct {
		Defs struct {
			EventType struct{ Enum []string }
		} `json:"$defs"`
	}
	if err := json.Unmarshal(raw, &schema); err != nil {
		t.Fatal(err)
	}
	catalog, err := os.ReadFile(filepath.Join(root, "docs", "events.md"))
	if err != nil {
		t.Fatal(err)
	}
	registered := make(map[string]bool)
	for _, kind := range schema.Defs.EventType.Enum {
		if registered[kind] {
			t.Errorf("duplicate schema event %q", kind)
		}
		registered[kind] = true
		if !strings.Contains(string(catalog), "`"+kind+"`") {
			t.Errorf("schema event %q is missing from docs/events.md", kind)
		}
	}
	server := filepath.Join(root, "server")
	tokens := token.NewFileSet()
	err = filepath.WalkDir(server, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && (entry.Name() == "conformance" || entry.Name() == "testdata" || entry.Name() == "bin") {
			return filepath.SkipDir
		}
		if entry.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(tokens, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(n ast.Node) bool {
			for _, kind := range eventNames(file.Name.Name, n) {
				if !registered[kind] || !strings.Contains(string(catalog), "`"+kind+"`") {
					t.Errorf("%s: event %q must be in the schema and catalog", tokens.Position(n.Pos()), kind)
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func eventNames(pkg string, n ast.Node) []string {
	switch n := n.(type) {
	case *ast.ValueSpec:
		var kind string
		switch typ := n.Type.(type) {
		case *ast.Ident:
			kind = typ.Name
		case *ast.SelectorExpr:
			kind = typ.Sel.Name
		}
		if kind == "EventType" {
			var names []string
			for _, v := range n.Values {
				names = append(names, literalEvent(v, "")...)
			}
			return names
		}
	case *ast.CallExpr:
		name := ""
		switch f := n.Fun.(type) {
		case *ast.Ident:
			name = f.Name
		case *ast.SelectorExpr:
			name = f.Sel.Name
		}
		switch {
		case (name == "NewEvent" || name == "EventType") && len(n.Args) > 0:
			return literalEvent(n.Args[0], "")
		case name == "publish" && pkg == "environment" && len(n.Args) == 5:
			return literalEvent(n.Args[1], "")
		case name == "publish" && pkg == "engine" && len(n.Args) == 1:
			return literalEvent(n.Args[0], "")
		case name == "save" && pkg == "mcpconfig" && len(n.Args) == 5:
			return literalEvent(n.Args[4], "")
		case name == "event" && pkg == "review" && len(n.Args) == 3:
			return literalEvent(n.Args[2], "review.")
		case name == "Emit" && len(n.Args) == 6:
			return literalEvent(n.Args[1], "")
		}
	case *ast.CompositeLit:
		if typ, ok := n.Type.(*ast.SelectorExpr); ok && typ.Sel.Name == "Event" {
			for _, elem := range n.Elts {
				if kv, ok := elem.(*ast.KeyValueExpr); ok {
					if key, ok := kv.Key.(*ast.Ident); ok && key.Name == "Type" {
						return literalEvent(kv.Value, "")
					}
				}
			}
		}
	}
	return nil
}

func literalEvent(expr ast.Expr, prefix string) []string {
	if lit, ok := expr.(*ast.BasicLit); ok && lit.Kind == token.STRING {
		value, err := strconv.Unquote(lit.Value)
		if err == nil {
			return []string{prefix + value}
		}
	}
	return nil
}
