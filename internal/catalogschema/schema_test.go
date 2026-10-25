package catalogschema_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/jsonschema-go/jsonschema"
	"go.yaml.in/yaml/v3"
)

func TestPublishedSchemaAcceptsCatalogExamples(t *testing.T) {
	t.Parallel()

	resolved := loadPublishedSchema(t)
	paths, err := filepath.Glob(filepath.Join("..", "..", "examples", "infrastructure-ops", ".slipramp", "*.yaml"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(paths) == 0 {
		t.Fatal("catalog example contains no YAML files")
	}

	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			t.Parallel()

			for index, document := range decodeYAMLDocuments(t, path) {
				if err := resolved.Validate(document); err != nil {
					t.Errorf("document %d does not match published schema: %v", index+1, err)
				}
			}
		})
	}
}

func TestPublishedSchemaRejectsStructuralErrors(t *testing.T) {
	t.Parallel()

	resolved := loadPublishedSchema(t)
	tests := map[string]string{
		"unknown field": `
apiVersion: slipramp.com/v1alpha1
kind: Runtime
metadata:
  name: tools
spec:
  image: example.com/tools:1
  typo: rejected
`,
		"wrong command type": `
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: operations
spec:
  actions:
    inspect:
      command: inspect
`,
		"unsupported kind": `
apiVersion: slipramp.com/v1alpha1
kind: Placement
metadata:
  name: test
spec: {}
`,
		"relative action documentation URL": `
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: operations
spec:
  actions:
    inspect:
      documentationUrl: /docs/inspect
      command: [inspect]
`,
		"oversized parameter title": `
apiVersion: slipramp.com/v1alpha1
kind: Target
metadata:
  name: operations
spec:
  actions:
    inspect:
      command: [inspect]
      parameters:
        environment:
          title: ` + strings.Repeat("x", 201) + `
          type: string
          argument: {}
`,
	}

	for name, contents := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var document any
			if err := yaml.Unmarshal([]byte(contents), &document); err != nil {
				t.Fatalf("yaml.Unmarshal() error = %v", err)
			}
			if err := resolved.Validate(document); err == nil {
				t.Fatal("published schema accepted structurally invalid catalog document")
			}
		})
	}
}

func loadPublishedSchema(t *testing.T) *jsonschema.Resolved {
	t.Helper()

	contents, err := os.ReadFile(filepath.Join("..", "..", "schemas", "catalog-v1alpha1.json"))
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	var document jsonschema.Schema
	if err := json.Unmarshal(contents, &document); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	resolved, err := document.Resolve(nil)
	if err != nil {
		t.Fatalf("Schema.Resolve() error = %v", err)
	}

	return resolved
}

func decodeYAMLDocuments(t *testing.T, path string) []any {
	t.Helper()

	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("os.ReadFile() error = %v", err)
	}

	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	documents := []any{}
	for {
		var document any
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		if document != nil {
			documents = append(documents, document)
		}
	}

	return documents
}
