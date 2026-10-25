package cli

import (
	"encoding/json"
	"io"

	"github.com/slipramp/slipramp/internal/catalog"
)

type jsonResult struct {
	SchemaVersion   int                 `json:"schemaVersion"`
	ValidationLevel string              `json:"validationLevel"`
	Valid           bool                `json:"valid"`
	Strict          bool                `json:"strict"`
	Summary         validationSummary   `json:"summary"`
	Diagnostics     catalog.Diagnostics `json:"diagnostics"`
}

func renderJSON(output io.Writer, result catalog.Result, summary validationSummary, strict bool) error {
	document := jsonResult{
		SchemaVersion:   1,
		ValidationLevel: string(result.Level),
		Valid:           !result.Diagnostics.HasErrors() && (!strict || !result.Diagnostics.HasWarnings()),
		Strict:          strict,
		Summary:         summary,
		Diagnostics:     result.Diagnostics,
	}
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	encoder.SetIndent("", "  ")

	return encoder.Encode(document)
}
