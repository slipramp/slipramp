package catalog

import (
	"strconv"
	"strings"
	"unicode/utf8"

	"go.yaml.in/yaml/v3"
)

type sourceLocations map[string]map[string]SourceRange

const sourceKeyPrefix = "@key:"

func resourceLocationKey(kind, name string) string {
	return kind + "/" + name
}

func (c Catalog) location(kind, name, fieldPath string, fallback Source) SourceRange {
	resource := c.locations[resourceLocationKey(kind, name)]
	if resource != nil {
		for candidate := fieldPath; candidate != ""; candidate = parentFieldPath(candidate) {
			if location, found := resource[candidate]; found {
				location.FieldPath = fieldPath

				return location
			}
		}
		if location, found := resource[""]; found {
			location.FieldPath = fieldPath

			return location
		}
	}

	return SourceRange{
		Path:      fallback.Path,
		Document:  fallback.Document,
		FieldPath: fieldPath,
	}
}

func parentFieldPath(value string) string {
	parent, _, found := strings.CutLast(value, ".")
	if !found {
		return ""
	}

	return parent
}

func indexDocumentSource(source Source, root *yaml.Node) map[string]SourceRange {
	locations := map[string]SourceRange{
		"": nodeRange(source, root, ""),
	}
	indexNodeSource(source, root, "", locations)

	return locations
}

func indexNodeSource(source Source, node *yaml.Node, fieldPath string, locations map[string]SourceRange) {
	switch node.Kind {
	case yaml.DocumentNode:
		for _, child := range node.Content {
			indexNodeSource(source, child, fieldPath, locations)
		}
	case yaml.MappingNode:
		for index := 0; index+1 < len(node.Content); index += 2 {
			key := node.Content[index]
			value := node.Content[index+1]
			childPath := key.Value
			if fieldPath != "" {
				childPath = fieldPath + "." + key.Value
			}
			locations[sourceKeyPrefix+childPath] = nodeRange(source, key, childPath)
			locations[childPath] = nodeRange(source, value, childPath)
			indexNodeSource(source, value, childPath, locations)
		}
	case yaml.SequenceNode:
		for index, child := range node.Content {
			childPath := fieldPath + "." + strconv.Itoa(index)
			locations[childPath] = nodeRange(source, child, childPath)
			indexNodeSource(source, child, childPath, locations)
		}
	}
}

func nodeRange(source Source, node *yaml.Node, fieldPath string) SourceRange {
	endLine := node.Line
	endColumn := node.Column
	if node.Kind == yaml.ScalarNode {
		endColumn += utf8.RuneCountInString(node.Value)
	}

	return SourceRange{
		Path:      source.Path,
		Document:  source.Document,
		Line:      node.Line,
		Column:    node.Column,
		EndLine:   endLine,
		EndColumn: endColumn,
		FieldPath: fieldPath,
	}
}
