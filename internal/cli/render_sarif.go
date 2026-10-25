package cli

import (
	"encoding/json"
	"io"
	"slices"

	"github.com/slipramp/slipramp/internal/catalog"
)

type sarifDocument struct {
	Version string     `json:"version"`
	Schema  string     `json:"$schema"`
	Runs    []sarifRun `json:"runs"`
}

type sarifRun struct {
	Tool    sarifTool     `json:"tool"`
	Results []sarifResult `json:"results"`
}

type sarifTool struct {
	Driver sarifDriver `json:"driver"`
}

type sarifDriver struct {
	Name  string      `json:"name"`
	Rules []sarifRule `json:"rules"`
}

type sarifRule struct {
	ID string `json:"id"`
}

type sarifResult struct {
	RuleID           string                 `json:"ruleId"`
	Level            string                 `json:"level"`
	Message          sarifMessage           `json:"message"`
	Locations        []sarifLocation        `json:"locations"`
	RelatedLocations []sarifRelatedLocation `json:"relatedLocations,omitempty"`
}

type sarifMessage struct {
	Text string `json:"text"`
}

type sarifLocation struct {
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

type sarifPhysicalLocation struct {
	ArtifactLocation sarifArtifactLocation `json:"artifactLocation"`
	Region           *sarifRegion          `json:"region,omitempty"`
}

type sarifArtifactLocation struct {
	URI string `json:"uri"`
}

type sarifRegion struct {
	StartLine   int `json:"startLine"`
	StartColumn int `json:"startColumn,omitempty"`
	EndLine     int `json:"endLine,omitempty"`
	EndColumn   int `json:"endColumn,omitempty"`
}

type sarifRelatedLocation struct {
	ID               int                   `json:"id"`
	Message          sarifMessage          `json:"message"`
	PhysicalLocation sarifPhysicalLocation `json:"physicalLocation"`
}

func renderSARIF(output io.Writer, result catalog.Result) error {
	codes := map[string]struct{}{}
	results := make([]sarifResult, 0, len(result.Diagnostics))
	for _, item := range result.Diagnostics {
		codes[item.Code] = struct{}{}
		related := make([]sarifRelatedLocation, 0, len(item.Related))
		for index, location := range item.Related {
			related = append(related, sarifRelatedLocation{
				ID:               index + 1,
				Message:          sarifMessage{Text: location.Message},
				PhysicalLocation: sarifPhysical(location.Location),
			})
		}
		results = append(results, sarifResult{
			RuleID:           item.Code,
			Level:            string(item.Severity),
			Message:          sarifMessage{Text: diagnosticText(item)},
			Locations:        []sarifLocation{{PhysicalLocation: sarifPhysical(item.Location)}},
			RelatedLocations: related,
		})
	}

	ruleIDs := make([]string, 0, len(codes))
	for code := range codes {
		ruleIDs = append(ruleIDs, code)
	}
	slices.Sort(ruleIDs)
	rules := make([]sarifRule, 0, len(ruleIDs))
	for _, code := range ruleIDs {
		rules = append(rules, sarifRule{ID: code})
	}

	document := sarifDocument{
		Version: "2.1.0",
		Schema:  "https://json.schemastore.org/sarif-2.1.0.json",
		Runs: []sarifRun{{
			Tool:    sarifTool{Driver: sarifDriver{Name: "slipramp/catalog", Rules: rules}},
			Results: results,
		}},
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	return encoder.Encode(document)
}

func diagnosticText(item catalog.Diagnostic) string {
	if item.Hint == "" {
		return item.Message
	}

	return item.Message + " Hint: " + item.Hint
}

func sarifPhysical(location catalog.SourceRange) sarifPhysicalLocation {
	physical := sarifPhysicalLocation{
		ArtifactLocation: sarifArtifactLocation{URI: location.Path},
	}
	if location.Line > 0 {
		physical.Region = &sarifRegion{
			StartLine:   location.Line,
			StartColumn: location.Column,
			EndLine:     location.EndLine,
			EndColumn:   location.EndColumn,
		}
	}

	return physical
}
