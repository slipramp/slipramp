// Package steps projects versioned step-report JSON into bounded Run reports.
package steps

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/slipramp/slipramp/internal/runreport/model"
)

const (
	ReportMediaType            = "application/vnd.slipramp.step-report+json"
	ReportKind      model.Kind = "step-report"

	SchemaVersion  = 1
	AdapterVersion = 1

	MaximumSourceSizeBytes int64 = 2 * 1024 * 1024
	MaximumSteps                 = 10_000

	maximumToolLength     = 64
	maximumTitleLength    = 256
	maximumStepIDLength   = 128
	maximumStepNameLength = 256
)

type Outcome string

const (
	OutcomeSucceeded Outcome = "succeeded"
	OutcomeWarning   Outcome = "warning"
	OutcomeFailed    Outcome = "failed"
	OutcomeSkipped   Outcome = "skipped"
	OutcomeCanceled  Outcome = "canceled"
	OutcomeNotRun    Outcome = "not-run"
)

type StepReport struct {
	model.Envelope
	Title   string            `json:"title,omitempty"`
	Summary StepReportSummary `json:"summary"`
	Steps   []Step            `json:"steps"`
}

type StepReportSummary struct {
	Outcome    Outcome           `json:"outcome"`
	DurationMS int64             `json:"durationMs"`
	Steps      StepOutcomeCounts `json:"steps"`
}

type StepOutcomeCounts struct {
	Total     int `json:"total"`
	Succeeded int `json:"succeeded"`
	Warning   int `json:"warning"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
	Canceled  int `json:"canceled"`
	NotRun    int `json:"notRun"`
}

type Step struct {
	ID         string  `json:"id"`
	Name       string  `json:"name"`
	Outcome    Outcome `json:"outcome"`
	DurationMS int64   `json:"durationMs"`
	Steps      []Step  `json:"steps,omitempty"`
}

type stepReportAdapter struct{}

func (stepReportAdapter) Kind() model.Kind {
	return ReportKind
}

func (stepReportAdapter) MaximumSourceSizeBytes() int64 {
	return MaximumSourceSizeBytes
}

func (stepReportAdapter) Decode(source io.Reader) (model.Report, error) {
	report, err := DecodeStepReport(source)
	if err != nil {
		return nil, err
	}

	return &report, nil
}

func (stepReportAdapter) NewReport() model.Report {
	return &StepReport{}
}

func (stepReportAdapter) NewSummary() model.Summary {
	return &StepReportSummary{}
}

func (stepReportAdapter) ValidateEnvelope(envelope model.Envelope) error {
	if envelope.Kind != ReportKind || envelope.SchemaVersion != SchemaVersion ||
		envelope.AdapterVersion != AdapterVersion || envelope.ToolVersion != "" ||
		(envelope.Mode != model.PreviewMode && envelope.Mode != model.ApplyMode) {
		return errors.New("step report envelope is invalid")
	}

	return model.ValidateRequiredString("tool", envelope.Tool, maximumToolLength)
}

func ReportFormat() model.Format {
	return stepReportAdapter{}
}

type stepReportSource struct {
	SchemaVersion int          `json:"schemaVersion"`
	Tool          string       `json:"tool"`
	Title         string       `json:"title"`
	Mode          model.Mode   `json:"mode"`
	Outcome       Outcome      `json:"outcome"`
	DurationMS    int64        `json:"durationMs"`
	Steps         []stepSource `json:"steps"`
}

type stepSource struct {
	ID         string       `json:"id"`
	Name       string       `json:"name"`
	Outcome    Outcome      `json:"outcome"`
	DurationMS int64        `json:"durationMs"`
	Steps      []stepSource `json:"steps"`
}

func DecodeStepReport(source io.Reader) (StepReport, error) {
	if source == nil {
		return StepReport{}, errors.New("step report source is required")
	}

	contents, err := io.ReadAll(io.LimitReader(source, MaximumSourceSizeBytes+1))
	if err != nil {
		return StepReport{}, fmt.Errorf("%w: %w", model.ErrSourceRead, err)
	}
	if int64(len(contents)) > MaximumSourceSizeBytes {
		return StepReport{}, fmt.Errorf(
			"%w: step report source must not exceed %d bytes",
			model.ErrSourceTooLarge,
			MaximumSourceSizeBytes,
		)
	}
	if !utf8.Valid(contents) {
		return StepReport{}, errors.New("step report source must be valid UTF-8 JSON")
	}

	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.DisallowUnknownFields()
	var document stepReportSource
	if err := decoder.Decode(&document); err != nil {
		return StepReport{}, fmt.Errorf("decode step report source: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return StepReport{}, errors.New("unexpected data after step report JSON object")
		}

		return StepReport{}, fmt.Errorf("decode trailing step report data: %w", err)
	}

	if document.SchemaVersion != SchemaVersion {
		return StepReport{}, fmt.Errorf("step report schemaVersion must be %d", SchemaVersion)
	}
	if err := model.ValidateRequiredString("tool", document.Tool, maximumToolLength); err != nil {
		return StepReport{}, err
	}
	if err := model.ValidateOptionalString("title", document.Title, maximumTitleLength); err != nil {
		return StepReport{}, err
	}
	if document.Mode != model.PreviewMode && document.Mode != model.ApplyMode {
		return StepReport{}, errors.New("step report mode is not supported")
	}
	if !validOutcome(document.Outcome) {
		return StepReport{}, errors.New("step report outcome is not supported")
	}
	if document.DurationMS < 0 {
		return StepReport{}, errors.New("step report durationMs must not be negative")
	}
	if document.Steps == nil {
		return StepReport{}, errors.New("step report steps are required")
	}

	steps, count, err := projectSteps(document.Steps, "steps", true)
	if err != nil {
		return StepReport{}, err
	}
	if count > MaximumSteps {
		return StepReport{}, fmt.Errorf("step report must contain at most %d steps", MaximumSteps)
	}

	envelope := model.Envelope{
		Kind:           ReportKind,
		SchemaVersion:  SchemaVersion,
		AdapterVersion: AdapterVersion,
		Tool:           document.Tool,
		Mode:           document.Mode,
	}
	report := StepReport{
		Envelope: envelope,
		Title:    document.Title,
		Summary: StepReportSummary{
			Outcome:    document.Outcome,
			DurationMS: document.DurationMS,
			Steps:      countLeafOutcomes(steps),
		},
		Steps: steps,
	}
	if _, err := model.Encode(report); err != nil {
		return StepReport{}, err
	}

	return report, nil
}

func projectSteps(source []stepSource, field string, allowChildren bool) ([]Step, int, error) {
	result := make([]Step, 0, len(source))
	seen := make(map[string]struct{}, len(source))
	count := 0
	for index, item := range source {
		itemField := fmt.Sprintf("%s[%d]", field, index)
		if err := model.ValidateRequiredString(itemField+".id", item.ID, maximumStepIDLength); err != nil {
			return nil, 0, err
		}
		if _, found := seen[item.ID]; found {
			return nil, 0, fmt.Errorf("%s.id is duplicated", itemField)
		}
		seen[item.ID] = struct{}{}
		if err := model.ValidateRequiredString(itemField+".name", item.Name, maximumStepNameLength); err != nil {
			return nil, 0, err
		}
		if !validOutcome(item.Outcome) {
			return nil, 0, fmt.Errorf("%s.outcome is not supported", itemField)
		}
		if item.DurationMS < 0 {
			return nil, 0, fmt.Errorf("%s.durationMs must not be negative", itemField)
		}
		if !allowChildren && len(item.Steps) != 0 {
			return nil, 0, fmt.Errorf("%s.steps exceeds the maximum nesting depth", itemField)
		}

		step := Step{
			ID:         item.ID,
			Name:       item.Name,
			Outcome:    item.Outcome,
			DurationMS: item.DurationMS,
		}
		count++
		if len(item.Steps) > 0 {
			children, childCount, err := projectSteps(item.Steps, itemField+".steps", false)
			if err != nil {
				return nil, 0, err
			}
			step.Steps = children
			count += childCount
		}
		result = append(result, step)
	}

	return result, count, nil
}

func (r StepReport) Metadata() model.Envelope {
	return r.Envelope
}

func (r StepReport) ReportSummary() model.Summary {
	return &r.Summary
}

func (r StepReport) Validate() error {
	if err := (stepReportAdapter{}).ValidateEnvelope(r.Envelope); err != nil {
		return err
	}
	if err := model.ValidateOptionalString("title", r.Title, maximumTitleLength); err != nil {
		return err
	}
	count, err := validateSteps(r.Steps, "steps", true)
	if err != nil {
		return err
	}
	if count > MaximumSteps {
		return fmt.Errorf("step report must contain at most %d steps", MaximumSteps)
	}
	if err := r.Summary.Validate(); err != nil {
		return err
	}
	if calculated := countLeafOutcomes(r.Steps); r.Summary.Steps != calculated {
		return errors.New("summary steps do not match report detail")
	}

	return nil
}

func (s StepReportSummary) ReportKind() model.Kind {
	return ReportKind
}

func (s StepReportSummary) Validate() error {
	if !validOutcome(s.Outcome) {
		return errors.New("summary outcome is not supported")
	}
	if s.DurationMS < 0 {
		return errors.New("summary durationMs must not be negative")
	}
	if s.Steps.Total < 0 || s.Steps.Succeeded < 0 || s.Steps.Warning < 0 ||
		s.Steps.Failed < 0 || s.Steps.Skipped < 0 || s.Steps.Canceled < 0 || s.Steps.NotRun < 0 {
		return errors.New("summary step counts must not be negative")
	}
	if s.Steps.Total != s.Steps.Succeeded+s.Steps.Warning+s.Steps.Failed+
		s.Steps.Skipped+s.Steps.Canceled+s.Steps.NotRun {
		return errors.New("summary total does not match its step outcome counts")
	}
	if s.Steps.Total > MaximumSteps {
		return fmt.Errorf("summary must contain at most %d steps", MaximumSteps)
	}

	return nil
}

func validateSteps(steps []Step, field string, allowChildren bool) (int, error) {
	if steps == nil {
		return 0, fmt.Errorf("%s are required", field)
	}
	seen := make(map[string]struct{}, len(steps))
	count := 0
	for index, step := range steps {
		stepField := fmt.Sprintf("%s[%d]", field, index)
		if err := model.ValidateRequiredString(stepField+".id", step.ID, maximumStepIDLength); err != nil {
			return 0, err
		}
		if _, found := seen[step.ID]; found {
			return 0, fmt.Errorf("%s.id is duplicated", stepField)
		}
		seen[step.ID] = struct{}{}
		if err := model.ValidateRequiredString(stepField+".name", step.Name, maximumStepNameLength); err != nil {
			return 0, err
		}
		if !validOutcome(step.Outcome) {
			return 0, fmt.Errorf("%s.outcome is not supported", stepField)
		}
		if step.DurationMS < 0 {
			return 0, fmt.Errorf("%s.durationMs must not be negative", stepField)
		}
		if !allowChildren && len(step.Steps) != 0 {
			return 0, fmt.Errorf("%s.steps exceeds the maximum nesting depth", stepField)
		}

		count++
		if len(step.Steps) > 0 {
			childCount, err := validateSteps(step.Steps, stepField+".steps", false)
			if err != nil {
				return 0, err
			}
			count += childCount
		}
	}

	return count, nil
}

func countLeafOutcomes(steps []Step) StepOutcomeCounts {
	counts := StepOutcomeCounts{}
	for _, step := range steps {
		if len(step.Steps) > 0 {
			counts.add(countLeafOutcomes(step.Steps))
			continue
		}

		counts.Total++
		switch step.Outcome {
		case OutcomeSucceeded:
			counts.Succeeded++
		case OutcomeWarning:
			counts.Warning++
		case OutcomeFailed:
			counts.Failed++
		case OutcomeSkipped:
			counts.Skipped++
		case OutcomeCanceled:
			counts.Canceled++
		case OutcomeNotRun:
			counts.NotRun++
		}
	}

	return counts
}

func (c *StepOutcomeCounts) add(other StepOutcomeCounts) {
	c.Total += other.Total
	c.Succeeded += other.Succeeded
	c.Warning += other.Warning
	c.Failed += other.Failed
	c.Skipped += other.Skipped
	c.Canceled += other.Canceled
	c.NotRun += other.NotRun
}

func validOutcome(outcome Outcome) bool {
	return outcome == OutcomeSucceeded || outcome == OutcomeWarning || outcome == OutcomeFailed ||
		outcome == OutcomeSkipped || outcome == OutcomeCanceled || outcome == OutcomeNotRun
}
