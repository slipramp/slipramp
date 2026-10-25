package catalog

import (
	"fmt"
	"slices"
)

// Severity describes whether a diagnostic prevents catalog acceptance.
type Severity string

const (
	SeverityError   Severity = "error"
	SeverityWarning Severity = "warning"
)

// SourceRange identifies a field in one repository catalog document. Lines and
// columns are one-based when known. FieldPath is the decoded YAML field path.
type SourceRange struct {
	Path      string `json:"path"`
	Document  int    `json:"document"`
	Line      int    `json:"line,omitempty"`
	Column    int    `json:"column,omitempty"`
	EndLine   int    `json:"endLine,omitempty"`
	EndColumn int    `json:"endColumn,omitempty"`
	FieldPath string `json:"fieldPath,omitempty"`
}

type RelatedLocation struct {
	Message  string      `json:"message"`
	Location SourceRange `json:"location"`
}

// Diagnostic is one stable, source-addressable catalog finding.
type Diagnostic struct {
	Code     string            `json:"code"`
	Severity Severity          `json:"severity"`
	Message  string            `json:"message"`
	Hint     string            `json:"hint,omitempty"`
	Location SourceRange       `json:"location"`
	Related  []RelatedLocation `json:"related,omitempty"`
}

type Diagnostics []Diagnostic

func (d Diagnostics) HasErrors() bool {
	return slices.ContainsFunc(d, func(item Diagnostic) bool {
		return item.Severity == SeverityError
	})
}

func (d Diagnostics) HasWarnings() bool {
	return slices.ContainsFunc(d, func(item Diagnostic) bool {
		return item.Severity == SeverityWarning
	})
}

func (d Diagnostics) Counts() (errors int, warnings int) {
	for _, item := range d {
		switch item.Severity {
		case SeverityError:
			errors++
		case SeverityWarning:
			warnings++
		}
	}

	return errors, warnings
}

// ErrorSummary returns a bounded, display-safe validation failure for Server
// refresh state. Rich consumers should use the diagnostics themselves.
func (d Diagnostics) ErrorSummary() string {
	errors, warnings := d.Counts()
	if errors == 0 {
		return ""
	}

	first := ""
	for _, item := range d {
		if item.Severity == SeverityError {
			first = fmt.Sprintf(
				"%s document %d: [%s] %s",
				item.Location.Path,
				item.Location.Document,
				item.Code,
				item.Message,
			)
			break
		}
	}

	if errors == 1 && warnings == 0 {
		return first
	}

	return fmt.Sprintf("%s (%d errors, %d warnings)", first, errors, warnings)
}

func sortDiagnostics(diagnostics Diagnostics) {
	slices.SortStableFunc(diagnostics, func(left, right Diagnostic) int {
		if comparison := compare(left.Location.Path, right.Location.Path); comparison != 0 {
			return comparison
		}
		if comparison := left.Location.Document - right.Location.Document; comparison != 0 {
			return comparison
		}
		if comparison := left.Location.Line - right.Location.Line; comparison != 0 {
			return comparison
		}
		if comparison := left.Location.Column - right.Location.Column; comparison != 0 {
			return comparison
		}
		if comparison := severityOrder(left.Severity) - severityOrder(right.Severity); comparison != 0 {
			return comparison
		}

		return compare(left.Code, right.Code)
	})
}

func compare(left, right string) int {
	switch {
	case left < right:
		return -1
	case left > right:
		return 1
	default:
		return 0
	}
}

func severityOrder(severity Severity) int {
	if severity == SeverityError {
		return 0
	}

	return 1
}
