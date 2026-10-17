package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"strings"

	"github.com/slipramp/slipramp/internal/placement"
	"go.yaml.in/yaml/v3"
)

type documentHeader struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
}

func Load(files fs.FS, placements placement.Set) (Catalog, error) {
	var result Catalog
	runtimeSources := map[string]Source{}
	targetSources := map[string]Source{}

	err := fs.WalkDir(files, Directory, func(
		filePath string,
		entry fs.DirEntry,
		walkErr error,
	) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() ||
			!entry.Type().IsRegular() ||
			!isYAMLFile(filePath) {
			return nil
		}

		contents, err := fs.ReadFile(files, filePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}

		return loadFile(
			contents,
			filePath,
			&result,
			runtimeSources,
			targetSources,
		)
	})
	if err != nil {
		return Catalog{}, fmt.Errorf("load catalog: %w", err)
	}
	if err := Validate(result, placements); err != nil {
		return Catalog{}, fmt.Errorf("load catalog: %w", err)
	}

	return result, nil
}

func isYAMLFile(filePath string) bool {
	extension := strings.ToLower(path.Ext(filePath))

	return extension == ".yaml" || extension == ".yml"
}

func loadFile(
	contents []byte,
	filePath string,
	result *Catalog,
	runtimeSources map[string]Source,
	targetSources map[string]Source,
) error {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))

	for documentNumber := 1; ; documentNumber++ {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return documentError(filePath, documentNumber, err)
		}
		if emptyDocument(document) {
			continue
		}

		if err := loadDocument(
			document,
			Source{Path: filePath, Document: documentNumber},
			result,
			runtimeSources,
			targetSources,
		); err != nil {
			return err
		}
	}
}

func loadDocument(
	document yaml.Node,
	source Source,
	result *Catalog,
	runtimeSources map[string]Source,
	targetSources map[string]Source,
) error {
	root := document.Content[0]
	if root.Kind != yaml.MappingNode {
		return documentError(
			source.Path,
			source.Document,
			errors.New("resource must be an object"),
		)
	}

	var header documentHeader
	if err := document.Decode(&header); err != nil {
		return documentError(source.Path, source.Document, err)
	}

	if header.APIVersion != APIVersion {
		return documentError(
			source.Path,
			source.Document,
			fmt.Errorf("apiVersion must be %q", APIVersion),
		)
	}

	if header.Metadata.Name == "" {
		return documentError(
			source.Path,
			source.Document,
			errors.New("metadata.name is required"),
		)
	}

	switch header.Kind {
	case RuntimeKind:
		if err := rejectDuplicate(
			RuntimeKind,
			header.Metadata.Name,
			source,
			runtimeSources,
		); err != nil {
			return err
		}

		var runtime Runtime
		if err := decodeStrict(document, &runtime); err != nil {
			return documentError(source.Path, source.Document, err)
		}
		runtime.Source = source
		result.Runtimes = append(result.Runtimes, runtime)
		runtimeSources[runtime.Metadata.Name] = source

	case TargetKind:
		if err := rejectDuplicate(
			TargetKind,
			header.Metadata.Name,
			source,
			targetSources,
		); err != nil {
			return err
		}

		var target Target
		if err := decodeStrict(document, &target); err != nil {
			return documentError(source.Path, source.Document, err)
		}
		target.Source = source
		result.Targets = append(result.Targets, target)
		targetSources[target.Metadata.Name] = source

	default:
		return documentError(
			source.Path,
			source.Document,
			fmt.Errorf("kind must be %q or %q", RuntimeKind, TargetKind),
		)
	}

	return nil
}

func decodeStrict(document yaml.Node, result any) error {
	encoded, err := yaml.Marshal(document.Content[0])
	if err != nil {
		return err
	}

	decoder := yaml.NewDecoder(bytes.NewReader(encoded))
	decoder.KnownFields(true)

	return decoder.Decode(result)
}

func rejectDuplicate(
	kind string,
	name string,
	source Source,
	known map[string]Source,
) error {
	previous, found := known[name]
	if !found {
		return nil
	}

	return documentError(
		source.Path,
		source.Document,
		fmt.Errorf(
			"%s %q is already defined in %s document %d",
			kind,
			name,
			previous.Path,
			previous.Document,
		),
	)
}

func emptyDocument(document yaml.Node) bool {
	if len(document.Content) == 0 {
		return true
	}

	root := document.Content[0]

	return root.Kind == yaml.ScalarNode && root.Tag == "!!null"
}

func documentError(filePath string, documentNumber int, err error) error {
	return fmt.Errorf("%s document %d: %w", filePath, documentNumber, err)
}
