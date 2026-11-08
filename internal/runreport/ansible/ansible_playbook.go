// Package ansible projects Ansible Runner event reports into bounded, redacted Run reports.
package ansible

import (
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	ansiblesource "github.com/slipramp/slipramp/internal/runreport/ansible/source"
	"github.com/slipramp/slipramp/internal/runreport/model"
)

const (
	PlaybookMediaType            = "application/vnd.slipramp.ansible-playbook+json"
	PlaybookKind      model.Kind = "ansible-playbook"

	SchemaVersion  = 1
	AdapterVersion = 1

	MaximumAnsiblePlaybookPlays   = 1_000
	MaximumAnsiblePlaybookTasks   = 20_000
	MaximumAnsiblePlaybookHosts   = 128
	MaximumAnsiblePlaybookResults = 100_000

	maximumAnsibleIdentifierLength = 256
	maximumAnsibleEventNameLength  = 128
	maximumAnsibleNameLength       = 512
	maximumAnsibleActionLength     = 256
	maximumAnsibleHostNameLength   = 253
	maximumAnsibleSourcePathLength = 1_024
	maximumAnsibleRawPathLength    = 4_096
	maximumAnsibleVersionLength    = 128

	ansibleRedactedName = "[redacted]"
)

type AnsibleOutcome string

const (
	AnsibleOutcomeOK          AnsibleOutcome = "ok"
	AnsibleOutcomeChanged     AnsibleOutcome = "changed"
	AnsibleOutcomeFailed      AnsibleOutcome = "failed"
	AnsibleOutcomeUnreachable AnsibleOutcome = "unreachable"
	AnsibleOutcomeSkipped     AnsibleOutcome = "skipped"
)

type AnsibleRunnerStatus string

const (
	AnsibleRunnerStatusSuccessful AnsibleRunnerStatus = "successful"
	AnsibleRunnerStatusFailed     AnsibleRunnerStatus = "failed"
	AnsibleRunnerStatusTimeout    AnsibleRunnerStatus = "timeout"
	AnsibleRunnerStatusCanceled   AnsibleRunnerStatus = "canceled"
)

type AnsiblePlaybookReport struct {
	model.Envelope
	Runner  AnsibleRunnerSummary   `json:"runner"`
	Summary AnsiblePlaybookSummary `json:"summary"`
	Plays   []AnsiblePlay          `json:"plays"`
}

type AnsibleRunnerSummary struct {
	Version  string              `json:"version,omitempty"`
	Status   AnsibleRunnerStatus `json:"status"`
	ExitCode int                 `json:"exitCode"`
}

type AnsiblePlaybookSummary struct {
	Complete   bool                 `json:"complete"`
	Hosts      int                  `json:"hosts"`
	Plays      int                  `json:"plays"`
	Tasks      int                  `json:"tasks"`
	Results    AnsibleOutcomeCounts `json:"results"`
	HostTotals []AnsibleHostTotals  `json:"hostTotals"`
}

type AnsibleOutcomeCounts struct {
	Total       int `json:"total"`
	OK          int `json:"ok"`
	Changed     int `json:"changed"`
	Failed      int `json:"failed"`
	Unreachable int `json:"unreachable"`
	Skipped     int `json:"skipped"`
}

type AnsibleHostTotals struct {
	Host        string `json:"host"`
	OK          int    `json:"ok"`
	Changed     int    `json:"changed"`
	Failed      int    `json:"failed"`
	Unreachable int    `json:"unreachable"`
	Skipped     int    `json:"skipped"`
	Rescued     int    `json:"rescued"`
	Ignored     int    `json:"ignored"`
}

type AnsiblePlay struct {
	Order int           `json:"order"`
	ID    string        `json:"id"`
	Name  string        `json:"name"`
	Tasks []AnsibleTask `json:"tasks"`
}

type AnsibleTask struct {
	Order      int                 `json:"order"`
	ID         string              `json:"id"`
	Name       string              `json:"name"`
	Action     string              `json:"action"`
	SourcePath string              `json:"sourcePath,omitempty"`
	Redacted   bool                `json:"redacted,omitempty"`
	Results    []AnsibleTaskResult `json:"results"`
}

type AnsibleTaskResult struct {
	Order          int            `json:"order"`
	Host           string         `json:"host"`
	Outcome        AnsibleOutcome `json:"outcome"`
	LoopItem       bool           `json:"loopItem,omitempty"`
	IgnoredFailure bool           `json:"ignoredFailure,omitempty"`
}

type ansiblePlaybookAdapter struct{}

func (ansiblePlaybookAdapter) Kind() model.Kind {
	return PlaybookKind
}

func (ansiblePlaybookAdapter) MaximumSourceSizeBytes() int64 {
	return ansiblesource.MaximumSourceSize
}

func (ansiblePlaybookAdapter) Decode(source io.Reader) (model.Report, error) {
	report, err := DecodeAnsiblePlaybook(source)
	if err != nil {
		return nil, err
	}

	return &report, nil
}

func (ansiblePlaybookAdapter) NewReport() model.Report {
	return &AnsiblePlaybookReport{}
}

func (ansiblePlaybookAdapter) NewSummary() model.Summary {
	return &AnsiblePlaybookSummary{}
}

func (ansiblePlaybookAdapter) ValidateEnvelope(envelope model.Envelope) error {
	return validateAnsiblePlaybookEnvelope(envelope)
}

func PlaybookFormat() model.Format {
	return ansiblePlaybookAdapter{}
}

func (r AnsiblePlaybookReport) Metadata() model.Envelope {
	return r.Envelope
}

func (r AnsiblePlaybookReport) ReportSummary() model.Summary {
	return &r.Summary
}

func (r AnsiblePlaybookReport) Validate() error {
	if err := validateAnsiblePlaybookEnvelope(r.Envelope); err != nil {
		return err
	}
	if err := r.Runner.validate(); err != nil {
		return err
	}
	if len(r.Plays) > MaximumAnsiblePlaybookPlays {
		return fmt.Errorf("plays must contain at most %d entries", MaximumAnsiblePlaybookPlays)
	}
	totalTasks := 0
	totalResults := 0
	for _, play := range r.Plays {
		totalTasks += len(play.Tasks)
		if totalTasks > MaximumAnsiblePlaybookTasks {
			return fmt.Errorf("report must contain at most %d tasks", MaximumAnsiblePlaybookTasks)
		}
		for _, task := range play.Tasks {
			totalResults += len(task.Results)
			if totalResults > MaximumAnsiblePlaybookResults {
				return fmt.Errorf("report must contain at most %d host-task results", MaximumAnsiblePlaybookResults)
			}
		}
	}

	playIDs := make(map[string]struct{}, len(r.Plays))
	taskIDs := make(map[string]struct{})
	resultHosts := make(map[string]struct{})
	counts := AnsibleOutcomeCounts{}
	previousPlayOrder := 0
	for playIndex, play := range r.Plays {
		field := fmt.Sprintf("plays[%d]", playIndex)
		if err := play.validate(field); err != nil {
			return err
		}
		if play.Order <= previousPlayOrder {
			return errors.New("plays must use event order")
		}
		previousPlayOrder = play.Order
		if _, found := playIDs[play.ID]; found {
			return fmt.Errorf("%s.id is duplicated", field)
		}
		playIDs[play.ID] = struct{}{}

		previousTaskOrder := 0
		for taskIndex, task := range play.Tasks {
			taskField := fmt.Sprintf("%s.tasks[%d]", field, taskIndex)
			if task.Order <= previousTaskOrder {
				return fmt.Errorf("%s.tasks must use event order", field)
			}
			previousTaskOrder = task.Order
			if _, found := taskIDs[task.ID]; found {
				return fmt.Errorf("%s.id is duplicated", taskField)
			}
			taskIDs[task.ID] = struct{}{}

			previousResultOrder := 0
			for resultIndex, result := range task.Results {
				resultField := fmt.Sprintf("%s.results[%d]", taskField, resultIndex)
				if err := result.validate(resultField); err != nil {
					return err
				}
				if result.Order <= previousResultOrder {
					return fmt.Errorf("%s.results must use event order", taskField)
				}
				previousResultOrder = result.Order
				counts.add(result.Outcome)
				resultHosts[result.Host] = struct{}{}
			}
		}
	}
	if err := r.Summary.Validate(); err != nil {
		return err
	}
	if r.Summary.Plays != len(r.Plays) || r.Summary.Tasks != totalTasks || r.Summary.Results != counts {
		return errors.New("ansible playbook summary does not match report detail")
	}
	summaryHosts := make(map[string]struct{}, len(r.Summary.HostTotals))
	for _, totals := range r.Summary.HostTotals {
		summaryHosts[totals.Host] = struct{}{}
	}
	for host := range resultHosts {
		if _, found := summaryHosts[host]; !found {
			return fmt.Errorf("result host %q is missing from summary hostTotals", host)
		}
	}
	if !r.Summary.Complete {
		calculated := calculatePartialAnsibleHostTotals(r.Plays)
		if !slices.Equal(calculated, r.Summary.HostTotals) {
			return errors.New("partial Ansible hostTotals do not match report detail")
		}
	}

	return nil
}

func validateAnsiblePlaybookEnvelope(envelope model.Envelope) error {
	if envelope.Kind != PlaybookKind ||
		envelope.SchemaVersion != SchemaVersion ||
		envelope.AdapterVersion != AdapterVersion ||
		envelope.Tool != "ansible" ||
		(envelope.Mode != model.PreviewMode && envelope.Mode != model.ApplyMode) {
		return errors.New("ansible playbook report envelope is invalid")
	}

	return validateAnsibleOptionalText("toolVersion", envelope.ToolVersion, maximumAnsibleVersionLength)
}

func (r AnsibleRunnerSummary) validate() error {
	if err := validateAnsibleOptionalText("runner.version", r.Version, maximumAnsibleVersionLength); err != nil {
		return err
	}
	if !validAnsibleRunnerStatus(r.Status) {
		return errors.New("runner.status is not supported")
	}
	if r.ExitCode < 0 || r.ExitCode > 255 {
		return errors.New("runner.exitCode must be between 0 and 255")
	}

	return nil
}

func validAnsibleRunnerStatus(status AnsibleRunnerStatus) bool {
	return status == AnsibleRunnerStatusSuccessful || status == AnsibleRunnerStatusFailed ||
		status == AnsibleRunnerStatusTimeout || status == AnsibleRunnerStatusCanceled
}

func (s AnsiblePlaybookSummary) ReportKind() model.Kind {
	return PlaybookKind
}

func (s AnsiblePlaybookSummary) Validate() error {
	if s.Hosts < 0 || s.Hosts > MaximumAnsiblePlaybookHosts ||
		s.Plays < 0 || s.Plays > MaximumAnsiblePlaybookPlays ||
		s.Tasks < 0 || s.Tasks > MaximumAnsiblePlaybookTasks {
		return errors.New("ansible playbook summary counts are outside report bounds")
	}
	if err := s.Results.validate(); err != nil {
		return err
	}
	if len(s.HostTotals) != s.Hosts {
		return errors.New("summary.hosts does not match summary.hostTotals")
	}

	previousHost := ""
	baseOutcomes := 0
	for index, totals := range s.HostTotals {
		if err := totals.validate(fmt.Sprintf("summary.hostTotals[%d]", index)); err != nil {
			return err
		}
		if index > 0 && previousHost >= totals.Host {
			return errors.New("summary.hostTotals must use deterministic order without duplicates")
		}
		previousHost = totals.Host
		baseOutcomes += totals.OK + totals.Failed + totals.Unreachable + totals.Skipped
		if baseOutcomes > ansiblesource.MaximumSourceEvents {
			return errors.New("summary.hostTotals exceed Ansible event bounds")
		}
	}

	return nil
}

func (c AnsibleOutcomeCounts) validate() error {
	if c.Total < 0 || c.OK < 0 || c.Changed < 0 || c.Failed < 0 || c.Unreachable < 0 || c.Skipped < 0 {
		return errors.New("summary.results counts must not be negative")
	}
	if c.Total != c.OK+c.Changed+c.Failed+c.Unreachable+c.Skipped {
		return errors.New("summary.results total does not match its outcome counts")
	}
	if c.Total > MaximumAnsiblePlaybookResults {
		return fmt.Errorf("summary.results must not exceed %d entries", MaximumAnsiblePlaybookResults)
	}

	return nil
}

func (c *AnsibleOutcomeCounts) add(outcome AnsibleOutcome) {
	c.Total++
	switch outcome {
	case AnsibleOutcomeOK:
		c.OK++
	case AnsibleOutcomeChanged:
		c.Changed++
	case AnsibleOutcomeFailed:
		c.Failed++
	case AnsibleOutcomeUnreachable:
		c.Unreachable++
	case AnsibleOutcomeSkipped:
		c.Skipped++
	}
}

func (t AnsibleHostTotals) validate(field string) error {
	if err := validateAnsibleRequiredText(field+".host", t.Host, maximumAnsibleHostNameLength); err != nil {
		return err
	}
	if t.OK < 0 || t.Changed < 0 || t.Failed < 0 || t.Unreachable < 0 || t.Skipped < 0 ||
		t.Rescued < 0 || t.Ignored < 0 {
		return fmt.Errorf("%s counts must not be negative", field)
	}
	if t.Changed > t.OK {
		return fmt.Errorf("%s.changed must not exceed ok", field)
	}
	for _, count := range []int{t.OK, t.Changed, t.Failed, t.Unreachable, t.Skipped, t.Rescued, t.Ignored} {
		if count > ansiblesource.MaximumSourceEvents {
			return fmt.Errorf("%s counts exceed Ansible event bounds", field)
		}
	}

	return nil
}

func (p AnsiblePlay) validate(field string) error {
	if p.Order <= 0 {
		return fmt.Errorf("%s.order must be positive", field)
	}
	if err := validateAnsibleRequiredText(field+".id", p.ID, maximumAnsibleIdentifierLength); err != nil {
		return err
	}
	if err := validateAnsibleRequiredText(field+".name", p.Name, maximumAnsibleNameLength); err != nil {
		return err
	}
	if p.Tasks == nil {
		return fmt.Errorf("%s.tasks is required", field)
	}

	for index, task := range p.Tasks {
		if err := task.validate(fmt.Sprintf("%s.tasks[%d]", field, index)); err != nil {
			return err
		}
	}

	return nil
}

func (t AnsibleTask) validate(field string) error {
	if t.Order <= 0 {
		return fmt.Errorf("%s.order must be positive", field)
	}
	if err := validateAnsibleRequiredText(field+".id", t.ID, maximumAnsibleIdentifierLength); err != nil {
		return err
	}
	if t.Redacted {
		if t.Name != ansibleRedactedName || t.Action != ansibleRedactedName || t.SourcePath != "" {
			return fmt.Errorf("%s redacted presentation is invalid", field)
		}
	} else {
		if err := validateAnsibleRequiredText(field+".name", t.Name, maximumAnsibleNameLength); err != nil {
			return err
		}
		if err := validateAnsibleRequiredText(field+".action", t.Action, maximumAnsibleActionLength); err != nil {
			return err
		}
		if t.SourcePath != "" {
			if err := validateAnsibleRequiredText(
				field+".sourcePath",
				t.SourcePath,
				maximumAnsibleSourcePathLength,
			); err != nil {
				return err
			}
			if path.IsAbs(t.SourcePath) || t.SourcePath == ".." || strings.HasPrefix(t.SourcePath, "../") ||
				path.Clean(t.SourcePath) != t.SourcePath {
				return fmt.Errorf("%s.sourcePath must be a canonical repository-relative path", field)
			}
		}
	}
	if t.Results == nil {
		return fmt.Errorf("%s.results is required", field)
	}

	return nil
}

func (r AnsibleTaskResult) validate(field string) error {
	if r.Order <= 0 {
		return fmt.Errorf("%s.order must be positive", field)
	}
	if err := validateAnsibleRequiredText(field+".host", r.Host, maximumAnsibleHostNameLength); err != nil {
		return err
	}
	switch r.Outcome {
	case AnsibleOutcomeOK, AnsibleOutcomeChanged, AnsibleOutcomeFailed,
		AnsibleOutcomeUnreachable, AnsibleOutcomeSkipped:
	default:
		return fmt.Errorf("%s.outcome is not supported", field)
	}
	if r.IgnoredFailure && r.Outcome != AnsibleOutcomeFailed {
		return fmt.Errorf("%s.ignoredFailure requires a failed outcome", field)
	}

	return nil
}

func calculatePartialAnsibleHostTotals(plays []AnsiblePlay) []AnsibleHostTotals {
	hosts := make(map[string]*AnsibleHostTotals)
	for _, play := range plays {
		for _, task := range play.Tasks {
			for _, result := range task.Results {
				totals, found := hosts[result.Host]
				if !found {
					totals = &AnsibleHostTotals{Host: result.Host}
					hosts[result.Host] = totals
				}
				switch result.Outcome {
				case AnsibleOutcomeOK:
					totals.OK++
				case AnsibleOutcomeChanged:
					totals.OK++
					totals.Changed++
				case AnsibleOutcomeFailed:
					if result.IgnoredFailure {
						totals.Ignored++
					} else {
						totals.Failed++
					}
				case AnsibleOutcomeUnreachable:
					totals.Unreachable++
				case AnsibleOutcomeSkipped:
					totals.Skipped++
				}
			}
		}
	}

	result := make([]AnsibleHostTotals, 0, len(hosts))
	for _, totals := range hosts {
		result = append(result, *totals)
	}
	slices.SortFunc(result, func(left, right AnsibleHostTotals) int {
		return strings.Compare(left.Host, right.Host)
	})

	return result
}

func validateAnsibleRequiredText(field string, value string, maximum int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}

	return validateAnsibleOptionalText(field, value, maximum)
}

func validateAnsibleOptionalText(field string, value string, maximum int) error {
	if utf8.RuneCountInString(value) > maximum {
		return fmt.Errorf("%s must not exceed %d characters", field, maximum)
	}
	for _, character := range value {
		if unicode.IsControl(character) || character == '\u2028' || character == '\u2029' {
			return fmt.Errorf("%s contains unsafe control characters", field)
		}
	}

	return nil
}
