package catalog

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path"
	"regexp"
	"strconv"
	"strings"

	"go.yaml.in/yaml/v3"
)

type documentHeader struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
}

type knownResource struct {
	location SourceRange
}

var (
	yamlLinePattern         = regexp.MustCompile(`line ([0-9]+)`) // yaml.v3 does not expose a structured parse error.
	yamlUnknownFieldPattern = regexp.MustCompile(`field ([^ ]+) not found in type`)
	yamlDuplicateKeyPattern = regexp.MustCompile(`mapping key "([^"]+)" already defined`)
)

// Decode discovers and decodes every independent catalog document. Document
// failures are returned as diagnostics; filesystem failures are operational
// errors because discovery could not be completed reliably.
func Decode(files fs.FS) (Catalog, Diagnostics, error) {
	result := Catalog{locations: sourceLocations{}}
	diagnostics := Diagnostics{}
	runtimeSources := map[string]knownResource{}
	targetSources := map[string]knownResource{}

	err := fs.WalkDir(files, Directory, func(filePath string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.IsDir() || !entry.Type().IsRegular() || !isYAMLFile(filePath) {
			return nil
		}

		contents, err := fs.ReadFile(files, filePath)
		if err != nil {
			return fmt.Errorf("read %s: %w", filePath, err)
		}

		diagnostics = append(
			diagnostics,
			decodeFile(contents, filePath, &result, runtimeSources, targetSources)...,
		)

		return nil
	})
	if err != nil {
		return Catalog{}, nil, fmt.Errorf("discover catalog: %w", err)
	}

	sortDiagnostics(diagnostics)

	return result, diagnostics, nil
}

func isYAMLFile(filePath string) bool {
	extension := strings.ToLower(path.Ext(filePath))

	return extension == ".yaml" || extension == ".yml"
}

func decodeFile(
	contents []byte,
	filePath string,
	result *Catalog,
	runtimeSources map[string]knownResource,
	targetSources map[string]knownResource,
) Diagnostics {
	decoder := yaml.NewDecoder(bytes.NewReader(contents))
	diagnostics := Diagnostics{}

	for documentNumber := 1; ; documentNumber++ {
		var document yaml.Node
		err := decoder.Decode(&document)
		if errors.Is(err, io.EOF) {
			return diagnostics
		}
		if err != nil {
			diagnostics = append(diagnostics, yamlDiagnostic(
				Source{Path: filePath, Document: documentNumber},
				"catalog.yaml.syntax",
				err,
				0,
			))

			return diagnostics
		}
		if emptyDocument(document) {
			continue
		}

		diagnostics = append(diagnostics, decodeDocument(
			document,
			Source{Path: filePath, Document: documentNumber},
			result,
			runtimeSources,
			targetSources,
		)...)
	}
}

func decodeDocument(
	document yaml.Node,
	source Source,
	result *Catalog,
	runtimeSources map[string]knownResource,
	targetSources map[string]knownResource,
) Diagnostics {
	root := document.Content[0]
	rootLocation := nodeRange(source, root, "")
	if root.Kind != yaml.MappingNode {
		return Diagnostics{{
			Code:     "catalog.yaml.document-type",
			Severity: SeverityError,
			Message:  "Catalog resource must be an object.",
			Location: rootLocation,
		}}
	}

	locations := indexDocumentSource(source, root)
	var header documentHeader
	if err := document.Decode(&header); err != nil {
		return Diagnostics{indexedYAMLDiagnostic(source, locations, err, 0)}
	}

	if header.APIVersion != APIVersion {
		return Diagnostics{{
			Code:     "catalog.api-version.unsupported",
			Severity: SeverityError,
			Message:  fmt.Sprintf("apiVersion must be %q.", APIVersion),
			Location: locationFromIndex(locations, "apiVersion", rootLocation),
		}}
	}

	if header.Metadata.Name == "" {
		return Diagnostics{{
			Code:     "catalog.metadata.name-required",
			Severity: SeverityError,
			Message:  "metadata.name is required.",
			Location: locationFromIndex(locations, "metadata", rootLocation),
		}}
	}

	var known map[string]knownResource
	switch header.Kind {
	case RuntimeKind:
		known = runtimeSources
	case TargetKind:
		known = targetSources
	default:
		return Diagnostics{{
			Code:     "catalog.resource.kind-unsupported",
			Severity: SeverityError,
			Message:  fmt.Sprintf("kind must be %q or %q.", RuntimeKind, TargetKind),
			Location: locationFromIndex(locations, "kind", rootLocation),
		}}
	}

	nameLocation := locationFromIndex(locations, "metadata.name", rootLocation)
	if previous, found := known[header.Metadata.Name]; found {
		return Diagnostics{{
			Code:     "catalog.resource.duplicate",
			Severity: SeverityError,
			Message:  fmt.Sprintf("%s %q is already defined.", header.Kind, header.Metadata.Name),
			Location: nameLocation,
			Related: []RelatedLocation{{
				Message:  "First definition is here.",
				Location: previous.location,
			}},
		}}
	}

	resourceKey := resourceLocationKey(header.Kind, header.Metadata.Name)
	result.locations[resourceKey] = locations
	known[header.Metadata.Name] = knownResource{location: nameLocation}

	switch header.Kind {
	case RuntimeKind:
		var runtime Runtime
		if err := decodeStrict(document, &runtime); err != nil {
			delete(result.locations, resourceKey)
			delete(known, header.Metadata.Name)

			return Diagnostics{indexedYAMLDiagnostic(source, locations, err, root.Line-1)}
		}
		runtime.Source = source
		result.Runtimes = append(result.Runtimes, runtime)
	case TargetKind:
		var target Target
		if err := decodeStrict(document, &target); err != nil {
			delete(result.locations, resourceKey)
			delete(known, header.Metadata.Name)

			return Diagnostics{indexedYAMLDiagnostic(source, locations, err, root.Line-1)}
		}
		target.Source = source
		result.Targets = append(result.Targets, target)
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

func emptyDocument(document yaml.Node) bool {
	if len(document.Content) == 0 {
		return true
	}

	root := document.Content[0]

	return root.Kind == yaml.ScalarNode && root.Tag == "!!null"
}

func yamlErrorCode(err error) string {
	message := err.Error()
	if strings.Contains(message, "not found in type") {
		return "catalog.yaml.unknown-field"
	}
	if strings.Contains(message, "already defined") {
		return "catalog.yaml.duplicate-key"
	}

	return "catalog.yaml.decode"
}

func yamlDiagnostic(source Source, code string, err error, lineOffset int) Diagnostic {
	line := 0
	match := yamlLinePattern.FindStringSubmatch(err.Error())
	if len(match) == 2 {
		line, _ = strconv.Atoi(match[1])
		line += lineOffset
	}

	return Diagnostic{
		Code:     code,
		Severity: SeverityError,
		Message:  err.Error(),
		Location: SourceRange{
			Path:     source.Path,
			Document: source.Document,
			Line:     line,
		},
	}
}

func indexedYAMLDiagnostic(
	source Source,
	locations map[string]SourceRange,
	err error,
	lineOffset int,
) Diagnostic {
	diagnostic := yamlDiagnostic(source, yamlErrorCode(err), err, lineOffset)
	fieldName := ""
	if match := yamlUnknownFieldPattern.FindStringSubmatch(err.Error()); len(match) == 2 {
		fieldName = match[1]
	} else if match := yamlDuplicateKeyPattern.FindStringSubmatch(err.Error()); len(match) == 2 {
		fieldName = match[1]
	}
	if fieldName == "" {
		return diagnostic
	}

	for fieldPath, location := range locations {
		if !strings.HasPrefix(fieldPath, sourceKeyPrefix) {
			continue
		}
		fieldPath = strings.TrimPrefix(fieldPath, sourceKeyPrefix)
		if (fieldPath == fieldName || strings.HasSuffix(fieldPath, "."+fieldName)) &&
			(diagnostic.Location.Line == 0 || location.Line == diagnostic.Location.Line) {
			diagnostic.Location = location

			return diagnostic
		}
	}

	return diagnostic
}

func locationFromIndex(locations map[string]SourceRange, fieldPath string, fallback SourceRange) SourceRange {
	if location, found := locations[fieldPath]; found {
		return location
	}

	fallback.FieldPath = fieldPath

	return fallback
}
