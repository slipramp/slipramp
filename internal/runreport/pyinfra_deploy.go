package runreport

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"unicode/utf8"
)

const (
	MaximumPyinfraOperations  = 10_000
	MaximumPyinfraHostEntries = 200_000

	maximumPyinfraNamesPerOperation = 32
	maximumPyinfraOperationName     = 512
	maximumPyinfraHostName          = 512
	maximumPyinfraOperationHash     = 256
)

type PyinfraDeployReport struct {
	ReportEnvelope
	Summary     PyinfraDeploySummary `json:"summary"`
	Operations  []PyinfraOperation   `json:"operations"`
	FailedHosts []string             `json:"failedHosts"`
}

type PyinfraDeploySummary struct {
	Plan    PyinfraPlanSummary    `json:"plan"`
	Results *PyinfraResultSummary `json:"results,omitempty"`
}

type PyinfraPlanSummary struct {
	TotalOperations           int `json:"totalOperations"`
	ChangedOperations         int `json:"changedOperations"`
	UnchangedOperations       int `json:"unchangedOperations"`
	AffectedHosts             int `json:"affectedHosts"`
	PlannedHostOperations     int `json:"plannedHostOperations"`
	ConditionalHostOperations int `json:"conditionalHostOperations"`
}

type PyinfraResultSummary struct {
	HostOperations          int `json:"hostOperations"`
	ChangedHostOperations   int `json:"changedHostOperations"`
	FailedHostOperations    int `json:"failedHostOperations"`
	UnchangedHostOperations int `json:"unchangedHostOperations"`
	FailedHosts             int `json:"failedHosts"`
}

type PyinfraOperation struct {
	Names                       []string                `json:"names"`
	HostsWithChanges            []string                `json:"hostsWithChanges"`
	HostsWithConditionalChanges []string                `json:"hostsWithConditionalChanges"`
	Result                      *PyinfraOperationResult `json:"result,omitempty"`
}

type PyinfraOperationResult struct {
	ChangedHosts   []string `json:"changedHosts"`
	FailedHosts    []string `json:"failedHosts"`
	UnchangedHosts []string `json:"unchangedHosts"`
}

type pyinfraDeployAdapter struct{}

func (pyinfraDeployAdapter) Kind() ReportKind {
	return PyinfraDeployKind
}

func (pyinfraDeployAdapter) MaximumSourceSizeBytes() int64 {
	return MaximumPyinfraDeploySourceSizeBytes
}

func (pyinfraDeployAdapter) Decode(source io.Reader) (Report, error) {
	report, err := DecodePyinfraDeploy(source)
	if err != nil {
		return nil, err
	}

	return &report, nil
}

func (pyinfraDeployAdapter) newReport() Report {
	return &PyinfraDeployReport{}
}

func (pyinfraDeployAdapter) newSummary() Summary {
	return &PyinfraDeploySummary{}
}

func (pyinfraDeployAdapter) validateEnvelope(envelope ReportEnvelope) error {
	if envelope.SchemaVersion != PyinfraDeploySchemaVersion ||
		envelope.AdapterVersion != PyinfraDeployAdapterVersion ||
		envelope.Tool != "pyinfra" || envelope.ToolVersion != "" ||
		(envelope.Mode != PreviewMode && envelope.Mode != ApplyMode) {
		return errors.New("pyinfra deploy report envelope is invalid")
	}

	return nil
}

type pyinfraDeploySource struct {
	Plan    []pyinfraPlanOperationSource `json:"plan"`
	Results json.RawMessage              `json:"results"`
}

type pyinfraPlanOperationSource struct {
	Hash                       string   `json:"op_hash"`
	Names                      []string `json:"names"`
	HostsWithChange            []string `json:"hosts_with_change"`
	HostsWithConditionalChange []string `json:"hosts_with_conditional_change"`
}

type pyinfraResultsSource struct {
	Operations  []pyinfraResultOperationSource `json:"operations"`
	Totals      *pyinfraResultTotalsSource     `json:"totals"`
	FailedHosts []string                       `json:"failed_hosts"`
}

type pyinfraResultOperationSource struct {
	Hash     string   `json:"op_hash"`
	Hosts    int      `json:"hosts"`
	Success  []string `json:"success"`
	Error    []string `json:"error"`
	NoChange []string `json:"no_change"`
}

type pyinfraResultTotalsSource struct {
	Hosts    int `json:"hosts"`
	Success  int `json:"success"`
	Error    int `json:"error"`
	NoChange int `json:"no_change"`
}

func DecodePyinfraDeploy(source io.Reader) (PyinfraDeployReport, error) {
	if source == nil {
		return PyinfraDeployReport{}, errors.New("pyinfra deploy source is required")
	}

	contents, err := io.ReadAll(io.LimitReader(source, MaximumPyinfraDeploySourceSizeBytes+1))
	if err != nil {
		return PyinfraDeployReport{}, fmt.Errorf("%w: %w", ErrSourceRead, err)
	}
	if int64(len(contents)) > MaximumPyinfraDeploySourceSizeBytes {
		return PyinfraDeployReport{}, fmt.Errorf(
			"%w: pyinfra deploy source must not exceed %d bytes",
			ErrSourceTooLarge,
			MaximumPyinfraDeploySourceSizeBytes,
		)
	}
	if !utf8.Valid(contents) {
		return PyinfraDeployReport{}, errors.New("pyinfra deploy source must be valid UTF-8 JSON")
	}

	decoder := json.NewDecoder(bytes.NewReader(contents))
	var document pyinfraDeploySource
	if err := decoder.Decode(&document); err != nil {
		return PyinfraDeployReport{}, fmt.Errorf("decode pyinfra deploy source: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return PyinfraDeployReport{}, errors.New("unexpected data after pyinfra deploy JSON object")
		}

		return PyinfraDeployReport{}, fmt.Errorf("decode trailing pyinfra deploy data: %w", err)
	}
	if document.Plan == nil {
		return PyinfraDeployReport{}, errors.New("pyinfra deploy source plan is required")
	}
	if document.Results == nil {
		return PyinfraDeployReport{}, errors.New("pyinfra deploy source results are required")
	}
	if len(document.Plan) > MaximumPyinfraOperations {
		return PyinfraDeployReport{}, fmt.Errorf(
			"plan must contain at most %d operations",
			MaximumPyinfraOperations,
		)
	}

	envelope := ReportEnvelope{
		Kind:           PyinfraDeployKind,
		SchemaVersion:  PyinfraDeploySchemaVersion,
		AdapterVersion: PyinfraDeployAdapterVersion,
		Tool:           "pyinfra",
		Mode:           PreviewMode,
	}
	report := PyinfraDeployReport{
		ReportEnvelope: envelope,
		Operations:     make([]PyinfraOperation, 0, len(document.Plan)),
		FailedHosts:    []string{},
	}
	operationIndexes := make(map[string]int, len(document.Plan))
	for index, sourceOperation := range document.Plan {
		operation, err := projectPyinfraPlanOperation(index, sourceOperation)
		if err != nil {
			return PyinfraDeployReport{}, err
		}
		if _, found := operationIndexes[sourceOperation.Hash]; found {
			return PyinfraDeployReport{}, fmt.Errorf("plan[%d].op_hash is duplicated", index)
		}
		operationIndexes[sourceOperation.Hash] = index
		report.Operations = append(report.Operations, operation)
	}

	if !bytes.Equal(bytes.TrimSpace(document.Results), []byte("null")) {
		report.Mode = ApplyMode
		if err := projectPyinfraResults(document.Results, operationIndexes, &report); err != nil {
			return PyinfraDeployReport{}, err
		}
	}

	summary, _ := calculatePyinfraSummary(report)
	report.Summary = summary
	if _, err := Encode(report); err != nil {
		return PyinfraDeployReport{}, err
	}

	return report, nil
}

func projectPyinfraPlanOperation(
	index int,
	source pyinfraPlanOperationSource,
) (PyinfraOperation, error) {
	if err := validateRequiredString(
		fmt.Sprintf("plan[%d].op_hash", index),
		source.Hash,
		maximumPyinfraOperationHash,
	); err != nil {
		return PyinfraOperation{}, err
	}
	names, err := normalizePyinfraStrings(
		fmt.Sprintf("plan[%d].names", index),
		source.Names,
		maximumPyinfraNamesPerOperation,
		maximumPyinfraOperationName,
		true,
	)
	if err != nil {
		return PyinfraOperation{}, err
	}
	hosts, err := normalizePyinfraStrings(
		fmt.Sprintf("plan[%d].hosts_with_change", index),
		source.HostsWithChange,
		MaximumPyinfraHostEntries,
		maximumPyinfraHostName,
		false,
	)
	if err != nil {
		return PyinfraOperation{}, err
	}
	conditionalHosts, err := normalizePyinfraStrings(
		fmt.Sprintf("plan[%d].hosts_with_conditional_change", index),
		source.HostsWithConditionalChange,
		MaximumPyinfraHostEntries,
		maximumPyinfraHostName,
		false,
	)
	if err != nil {
		return PyinfraOperation{}, err
	}
	if overlap := firstPyinfraOverlap(hosts, conditionalHosts); overlap != "" {
		return PyinfraOperation{}, fmt.Errorf("plan[%d] host lists overlap", index)
	}

	return PyinfraOperation{
		Names:                       names,
		HostsWithChanges:            hosts,
		HostsWithConditionalChanges: conditionalHosts,
	}, nil
}

func projectPyinfraResults(
	encoded json.RawMessage,
	operationIndexes map[string]int,
	report *PyinfraDeployReport,
) error {
	var source pyinfraResultsSource
	if err := json.Unmarshal(encoded, &source); err != nil {
		return fmt.Errorf("decode pyinfra deploy results: %w", err)
	}
	if source.Operations == nil || source.Totals == nil || source.FailedHosts == nil {
		return errors.New("pyinfra deploy results operations, totals, and failed_hosts are required")
	}
	if len(source.Operations) != len(report.Operations) {
		return errors.New("pyinfra deploy results must contain every planned operation")
	}

	seen := make(map[string]struct{}, len(source.Operations))
	for index, sourceOperation := range source.Operations {
		if err := validateRequiredString(
			fmt.Sprintf("results.operations[%d].op_hash", index),
			sourceOperation.Hash,
			maximumPyinfraOperationHash,
		); err != nil {
			return err
		}
		operationIndex, found := operationIndexes[sourceOperation.Hash]
		if !found {
			return fmt.Errorf("results.operations[%d].op_hash does not match a planned operation", index)
		}
		if _, found := seen[sourceOperation.Hash]; found {
			return fmt.Errorf("results.operations[%d].op_hash is duplicated", index)
		}
		seen[sourceOperation.Hash] = struct{}{}

		result, err := projectPyinfraOperationResult(index, sourceOperation)
		if err != nil {
			return err
		}
		report.Operations[operationIndex].Result = &result
	}

	failedHosts, err := normalizePyinfraStrings(
		"results.failed_hosts",
		source.FailedHosts,
		MaximumPyinfraHostEntries,
		maximumPyinfraHostName,
		false,
	)
	if err != nil {
		return err
	}
	report.FailedHosts = failedHosts

	summary, _ := calculatePyinfraSummary(*report)
	wantTotals := PyinfraResultSummary{
		HostOperations:          source.Totals.Hosts,
		ChangedHostOperations:   source.Totals.Success,
		FailedHostOperations:    source.Totals.Error,
		UnchangedHostOperations: source.Totals.NoChange,
		FailedHosts:             len(failedHosts),
	}
	if summary.Results == nil || *summary.Results != wantTotals {
		return errors.New("pyinfra deploy results totals do not match operation outcomes")
	}

	return nil
}

func projectPyinfraOperationResult(
	index int,
	source pyinfraResultOperationSource,
) (PyinfraOperationResult, error) {
	changed, err := normalizePyinfraStrings(
		fmt.Sprintf("results.operations[%d].success", index),
		source.Success,
		MaximumPyinfraHostEntries,
		maximumPyinfraHostName,
		false,
	)
	if err != nil {
		return PyinfraOperationResult{}, err
	}
	failed, err := normalizePyinfraStrings(
		fmt.Sprintf("results.operations[%d].error", index),
		source.Error,
		MaximumPyinfraHostEntries,
		maximumPyinfraHostName,
		false,
	)
	if err != nil {
		return PyinfraOperationResult{}, err
	}
	unchanged, err := normalizePyinfraStrings(
		fmt.Sprintf("results.operations[%d].no_change", index),
		source.NoChange,
		MaximumPyinfraHostEntries,
		maximumPyinfraHostName,
		false,
	)
	if err != nil {
		return PyinfraOperationResult{}, err
	}
	if firstPyinfraOverlap(changed, failed) != "" || firstPyinfraOverlap(changed, unchanged) != "" ||
		firstPyinfraOverlap(failed, unchanged) != "" {
		return PyinfraOperationResult{}, fmt.Errorf("results.operations[%d] host outcome lists overlap", index)
	}
	if source.Hosts != len(changed)+len(failed)+len(unchanged) {
		return PyinfraOperationResult{}, fmt.Errorf(
			"results.operations[%d].hosts does not match its host outcomes",
			index,
		)
	}

	return PyinfraOperationResult{
		ChangedHosts:   changed,
		FailedHosts:    failed,
		UnchangedHosts: unchanged,
	}, nil
}

func (r PyinfraDeployReport) Metadata() ReportEnvelope {
	return r.ReportEnvelope
}

func (r PyinfraDeployReport) ReportSummary() Summary {
	return &r.Summary
}

func (r PyinfraDeployReport) Validate() error {
	if err := r.ReportEnvelope.Validate(); err != nil {
		return err
	}
	if len(r.Operations) > MaximumPyinfraOperations {
		return fmt.Errorf("operations must contain at most %d entries", MaximumPyinfraOperations)
	}
	for index, operation := range r.Operations {
		if err := operation.validate(fmt.Sprintf("operations[%d]", index), r.Mode); err != nil {
			return err
		}
	}
	if err := validateCanonicalPyinfraStrings(
		"failedHosts",
		r.FailedHosts,
		MaximumPyinfraHostEntries,
		maximumPyinfraHostName,
		false,
	); err != nil {
		return err
	}
	if r.Mode == PreviewMode && len(r.FailedHosts) != 0 {
		return errors.New("failedHosts is not valid in preview mode")
	}

	calculated, hostEntries := calculatePyinfraSummary(r)
	if hostEntries > MaximumPyinfraHostEntries {
		return fmt.Errorf("report must contain at most %d host entries", MaximumPyinfraHostEntries)
	}
	if r.Summary.Plan != calculated.Plan {
		return errors.New("summary plan does not match report detail")
	}
	if (r.Summary.Results == nil) != (calculated.Results == nil) ||
		(r.Summary.Results != nil && *r.Summary.Results != *calculated.Results) {
		return errors.New("summary results do not match report detail")
	}
	if err := r.Summary.Validate(); err != nil {
		return err
	}

	return nil
}

func (s PyinfraDeploySummary) ReportKind() ReportKind {
	return PyinfraDeployKind
}

func (s PyinfraDeploySummary) Validate() error {
	if err := s.Plan.validate(); err != nil {
		return err
	}
	if s.Results != nil {
		return s.Results.validate()
	}

	return nil
}

func (s PyinfraPlanSummary) validate() error {
	if s.TotalOperations < 0 || s.ChangedOperations < 0 || s.UnchangedOperations < 0 ||
		s.AffectedHosts < 0 || s.PlannedHostOperations < 0 || s.ConditionalHostOperations < 0 {
		return errors.New("summary.plan counts must not be negative")
	}
	if s.TotalOperations != s.ChangedOperations+s.UnchangedOperations {
		return errors.New("summary.plan totalOperations does not match its operation counts")
	}
	if s.TotalOperations > MaximumPyinfraOperations ||
		s.PlannedHostOperations+s.ConditionalHostOperations > MaximumPyinfraHostEntries {
		return errors.New("summary.plan exceeds pyinfra report bounds")
	}

	return nil
}

func (s PyinfraResultSummary) validate() error {
	if s.HostOperations < 0 || s.ChangedHostOperations < 0 || s.FailedHostOperations < 0 ||
		s.UnchangedHostOperations < 0 || s.FailedHosts < 0 {
		return errors.New("summary.results counts must not be negative")
	}
	if s.HostOperations != s.ChangedHostOperations+s.FailedHostOperations+s.UnchangedHostOperations {
		return errors.New("summary.results hostOperations does not match its outcome counts")
	}
	if s.HostOperations+s.FailedHosts > MaximumPyinfraHostEntries {
		return errors.New("summary.results exceeds pyinfra report bounds")
	}

	return nil
}

func (o PyinfraOperation) validate(field string, mode ReportMode) error {
	if err := validateCanonicalPyinfraStrings(
		field+".names",
		o.Names,
		maximumPyinfraNamesPerOperation,
		maximumPyinfraOperationName,
		true,
	); err != nil {
		return err
	}
	if err := validateCanonicalPyinfraStrings(
		field+".hostsWithChanges",
		o.HostsWithChanges,
		MaximumPyinfraHostEntries,
		maximumPyinfraHostName,
		false,
	); err != nil {
		return err
	}
	if err := validateCanonicalPyinfraStrings(
		field+".hostsWithConditionalChanges",
		o.HostsWithConditionalChanges,
		MaximumPyinfraHostEntries,
		maximumPyinfraHostName,
		false,
	); err != nil {
		return err
	}
	if firstPyinfraOverlap(o.HostsWithChanges, o.HostsWithConditionalChanges) != "" {
		return fmt.Errorf("%s planned host lists overlap", field)
	}

	if mode == PreviewMode {
		if o.Result != nil {
			return fmt.Errorf("%s.result is not valid in preview mode", field)
		}

		return nil
	}
	if o.Result == nil {
		return fmt.Errorf("%s.result is required in apply mode", field)
	}

	return o.Result.validate(field + ".result")
}

func (r PyinfraOperationResult) validate(field string) error {
	for _, item := range []struct {
		name   string
		values []string
	}{
		{name: "changedHosts", values: r.ChangedHosts},
		{name: "failedHosts", values: r.FailedHosts},
		{name: "unchangedHosts", values: r.UnchangedHosts},
	} {
		if err := validateCanonicalPyinfraStrings(
			field+"."+item.name,
			item.values,
			MaximumPyinfraHostEntries,
			maximumPyinfraHostName,
			false,
		); err != nil {
			return err
		}
	}
	if firstPyinfraOverlap(r.ChangedHosts, r.FailedHosts) != "" ||
		firstPyinfraOverlap(r.ChangedHosts, r.UnchangedHosts) != "" ||
		firstPyinfraOverlap(r.FailedHosts, r.UnchangedHosts) != "" {
		return fmt.Errorf("%s host outcome lists overlap", field)
	}

	return nil
}

func calculatePyinfraSummary(report PyinfraDeployReport) (PyinfraDeploySummary, int) {
	plan := PyinfraPlanSummary{TotalOperations: len(report.Operations)}
	affectedHosts := map[string]struct{}{}
	result := PyinfraResultSummary{FailedHosts: len(report.FailedHosts)}
	hostEntries := len(report.FailedHosts)
	for _, operation := range report.Operations {
		planned := len(operation.HostsWithChanges)
		conditional := len(operation.HostsWithConditionalChanges)
		plan.PlannedHostOperations += planned
		plan.ConditionalHostOperations += conditional
		hostEntries += planned + conditional
		if planned+conditional == 0 {
			plan.UnchangedOperations++
		} else {
			plan.ChangedOperations++
		}
		for _, host := range operation.HostsWithChanges {
			affectedHosts[host] = struct{}{}
		}
		for _, host := range operation.HostsWithConditionalChanges {
			affectedHosts[host] = struct{}{}
		}

		if operation.Result != nil {
			changed := len(operation.Result.ChangedHosts)
			failed := len(operation.Result.FailedHosts)
			unchanged := len(operation.Result.UnchangedHosts)
			result.ChangedHostOperations += changed
			result.FailedHostOperations += failed
			result.UnchangedHostOperations += unchanged
			hostEntries += changed + failed + unchanged
		}
	}
	plan.AffectedHosts = len(affectedHosts)
	result.HostOperations = result.ChangedHostOperations + result.FailedHostOperations +
		result.UnchangedHostOperations

	summary := PyinfraDeploySummary{Plan: plan}
	if report.Mode == ApplyMode {
		summary.Results = &result
	}

	return summary, hostEntries
}

func normalizePyinfraStrings(
	field string,
	values []string,
	maximumCount int,
	maximumLength int,
	requireNonempty bool,
) ([]string, error) {
	if values == nil || requireNonempty && len(values) == 0 {
		return nil, fmt.Errorf("%s is required", field)
	}
	if len(values) > maximumCount {
		return nil, fmt.Errorf("%s must contain at most %d entries", field, maximumCount)
	}

	result := slices.Clone(values)
	slices.Sort(result)
	for index, value := range result {
		if err := validateRequiredString(field, value, maximumLength); err != nil {
			return nil, err
		}
		if index > 0 && result[index-1] == value {
			return nil, fmt.Errorf("%s contains a duplicate entry", field)
		}
	}

	return result, nil
}

func validateCanonicalPyinfraStrings(
	field string,
	values []string,
	maximumCount int,
	maximumLength int,
	requireNonempty bool,
) error {
	if values == nil || requireNonempty && len(values) == 0 {
		return fmt.Errorf("%s is required", field)
	}
	if len(values) > maximumCount {
		return fmt.Errorf("%s must contain at most %d entries", field, maximumCount)
	}
	for index, value := range values {
		if err := validateRequiredString(field, value, maximumLength); err != nil {
			return err
		}
		if index > 0 && values[index-1] >= value {
			return fmt.Errorf("%s must use deterministic order without duplicates", field)
		}
	}

	return nil
}

func firstPyinfraOverlap(left, right []string) string {
	for leftIndex, rightIndex := 0, 0; leftIndex < len(left) && rightIndex < len(right); {
		switch {
		case left[leftIndex] < right[rightIndex]:
			leftIndex++
		case left[leftIndex] > right[rightIndex]:
			rightIndex++
		default:
			return left[leftIndex]
		}
	}

	return ""
}
