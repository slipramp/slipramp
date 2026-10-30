package runreport

import (
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	MaximumInfrastructureResourceEntries = 20_000
	MaximumInfrastructureOutputEntries   = 10_000
	MaximumInfrastructureCheckEntries    = 10_000

	maximumInfrastructureToolVersionLength = 128
	maximumResourceAddressLength           = 2_048
	maximumResourceModeLength              = 64
	maximumResourceTypeLength              = 256
	maximumResourceNameLength              = 256
	maximumProviderNameLength              = 512
	maximumResourceActionReasonLength      = 256
)

type InfrastructureAction string

const (
	InfrastructureActionNoOp    InfrastructureAction = "no-op"
	InfrastructureActionCreate  InfrastructureAction = "create"
	InfrastructureActionRead    InfrastructureAction = "read"
	InfrastructureActionUpdate  InfrastructureAction = "update"
	InfrastructureActionDelete  InfrastructureAction = "delete"
	InfrastructureActionForget  InfrastructureAction = "forget"
	InfrastructureActionReplace InfrastructureAction = "replace"
)

type InfrastructureReplacementOrder string

const (
	InfrastructureReplacementDestroyBeforeCreate InfrastructureReplacementOrder = "destroy-before-create"
	InfrastructureReplacementCreateBeforeDestroy InfrastructureReplacementOrder = "create-before-destroy"
)

type InfrastructurePlanReport struct {
	ReportEnvelope
	Summary         InfrastructurePlanSummary      `json:"summary"`
	ResourceChanges []InfrastructureResourceChange `json:"resourceChanges"`
	ResourceDrift   []InfrastructureResourceChange `json:"resourceDrift"`
}

type InfrastructurePlanSummary struct {
	reportKind       ReportKind
	ResourceChanges  InfrastructureActionCounts `json:"resourceChanges"`
	DriftedResources int                        `json:"driftedResources"`
	Checks           InfrastructureCheckCounts  `json:"checks"`
	OutputChanges    InfrastructureActionCounts `json:"outputChanges"`
	Applyable        *bool                      `json:"applyable,omitempty"`
	Complete         *bool                      `json:"complete,omitempty"`
	Errored          *bool                      `json:"errored,omitempty"`
}

type InfrastructureActionCounts struct {
	Total   int `json:"total"`
	NoOp    int `json:"noOp"`
	Create  int `json:"create"`
	Read    int `json:"read"`
	Update  int `json:"update"`
	Delete  int `json:"delete"`
	Forget  int `json:"forget"`
	Replace int `json:"replace"`
}

type InfrastructureCheckCounts struct {
	Total   int `json:"total"`
	Pass    int `json:"pass"`
	Fail    int `json:"fail"`
	Error   int `json:"error"`
	Unknown int `json:"unknown"`
}

type InfrastructureResourceChange struct {
	Address           string                         `json:"address"`
	ReplacedObjectKey string                         `json:"replacedObjectKey,omitempty"`
	PreviousAddress   string                         `json:"previousAddress,omitempty"`
	ModuleAddress     string                         `json:"moduleAddress,omitempty"`
	Mode              string                         `json:"mode"`
	Type              string                         `json:"type"`
	Name              string                         `json:"name"`
	ProviderName      string                         `json:"providerName"`
	Action            InfrastructureAction           `json:"action"`
	ReplacementOrder  InfrastructureReplacementOrder `json:"replacementOrder,omitempty"`
	ActionReason      string                         `json:"actionReason,omitempty"`
}

type infrastructurePlanAdapter struct {
	kind ReportKind
	tool string
}

func (a infrastructurePlanAdapter) Kind() ReportKind {
	return a.kind
}

func (infrastructurePlanAdapter) MaximumSourceSizeBytes() int64 {
	return MaximumInfrastructurePlanSourceSizeBytes
}

func (a infrastructurePlanAdapter) Decode(source io.Reader) (Report, error) {
	report, err := decodeInfrastructurePlan(source, a.kind, a.tool)
	if err != nil {
		return nil, err
	}

	return &report, nil
}

func (infrastructurePlanAdapter) newReport() Report {
	return &InfrastructurePlanReport{}
}

func (a infrastructurePlanAdapter) newSummary() Summary {
	return &InfrastructurePlanSummary{reportKind: a.kind}
}

func (a infrastructurePlanAdapter) validateEnvelope(envelope ReportEnvelope) error {
	if envelope.SchemaVersion != InfrastructurePlanSchemaVersion ||
		envelope.AdapterVersion != InfrastructurePlanAdapterVersion ||
		envelope.Tool != a.tool || envelope.Mode != PreviewMode {
		return errors.New("infrastructure plan report envelope is invalid")
	}

	return validateOptionalString("toolVersion", envelope.ToolVersion, maximumInfrastructureToolVersionLength)
}

type infrastructurePlanSource struct {
	FormatVersion    string                                `json:"format_version"`
	TerraformVersion string                                `json:"terraform_version"`
	Applyable        *bool                                 `json:"applyable"`
	Complete         *bool                                 `json:"complete"`
	Errored          *bool                                 `json:"errored"`
	ResourceChanges  []infrastructureResourceChangeSource  `json:"resource_changes"`
	ResourceDrift    []infrastructureResourceChangeSource  `json:"resource_drift"`
	OutputChanges    map[string]infrastructureChangeSource `json:"output_changes"`
	Checks           []infrastructureCheckSource           `json:"checks"`
}

type infrastructureResourceChangeSource struct {
	Address         string                     `json:"address"`
	Deposed         string                     `json:"deposed"`
	PreviousAddress string                     `json:"previous_address"`
	ModuleAddress   string                     `json:"module_address"`
	Mode            string                     `json:"mode"`
	Type            string                     `json:"type"`
	Name            string                     `json:"name"`
	ProviderName    string                     `json:"provider_name"`
	Change          infrastructureChangeSource `json:"change"`
	ActionReason    string                     `json:"action_reason"`
}

type infrastructureChangeSource struct {
	Actions []string `json:"actions"`
}

type infrastructureCheckSource struct {
	Status    string                              `json:"status"`
	Instances []infrastructureCheckInstanceSource `json:"instances"`
}

type infrastructureCheckInstanceSource struct {
	Status string `json:"status"`
}

func DecodeTerraformPlan(source io.Reader) (InfrastructurePlanReport, error) {
	return decodeInfrastructurePlan(source, TerraformPlanKind, "terraform")
}

func DecodeOpenTofuPlan(source io.Reader) (InfrastructurePlanReport, error) {
	return decodeInfrastructurePlan(source, OpenTofuPlanKind, "opentofu")
}

func decodeInfrastructurePlan(
	source io.Reader,
	kind ReportKind,
	tool string,
) (InfrastructurePlanReport, error) {
	if source == nil {
		return InfrastructurePlanReport{}, errors.New("infrastructure plan source is required")
	}

	contents, err := io.ReadAll(io.LimitReader(source, MaximumInfrastructurePlanSourceSizeBytes+1))
	if err != nil {
		return InfrastructurePlanReport{}, fmt.Errorf("%w: %w", ErrSourceRead, err)
	}
	if int64(len(contents)) > MaximumInfrastructurePlanSourceSizeBytes {
		return InfrastructurePlanReport{}, fmt.Errorf(
			"%w: infrastructure plan source must not exceed %d bytes",
			ErrSourceTooLarge,
			MaximumInfrastructurePlanSourceSizeBytes,
		)
	}
	if !utf8.Valid(contents) {
		return InfrastructurePlanReport{}, errors.New("infrastructure plan source must be valid UTF-8 JSON")
	}

	decoder := json.NewDecoder(bytes.NewReader(contents))
	var document infrastructurePlanSource
	if err := decoder.Decode(&document); err != nil {
		return InfrastructurePlanReport{}, fmt.Errorf("decode infrastructure plan source: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return InfrastructurePlanReport{}, errors.New("unexpected data after infrastructure plan JSON object")
		}

		return InfrastructurePlanReport{}, fmt.Errorf("decode trailing infrastructure plan data: %w", err)
	}

	if err := validateInfrastructureFormatVersion(document.FormatVersion); err != nil {
		return InfrastructurePlanReport{}, err
	}
	if document.ResourceChanges == nil && document.ResourceDrift == nil && document.OutputChanges == nil &&
		document.Checks == nil && document.Applyable == nil && document.Complete == nil && document.Errored == nil {
		return InfrastructurePlanReport{}, errors.New("infrastructure plan source does not contain a plan representation")
	}
	if err := validateOptionalString(
		"terraform_version",
		document.TerraformVersion,
		maximumInfrastructureToolVersionLength,
	); err != nil {
		return InfrastructurePlanReport{}, err
	}
	if len(document.ResourceChanges)+len(document.ResourceDrift) > MaximumInfrastructureResourceEntries {
		return InfrastructurePlanReport{}, fmt.Errorf(
			"resource_changes and resource_drift must contain at most %d entries in total",
			MaximumInfrastructureResourceEntries,
		)
	}
	if len(document.OutputChanges) > MaximumInfrastructureOutputEntries {
		return InfrastructurePlanReport{}, fmt.Errorf(
			"output_changes must contain at most %d entries",
			MaximumInfrastructureOutputEntries,
		)
	}

	envelope := ReportEnvelope{
		Kind:           kind,
		SchemaVersion:  InfrastructurePlanSchemaVersion,
		AdapterVersion: InfrastructurePlanAdapterVersion,
		Tool:           tool,
		ToolVersion:    document.TerraformVersion,
		Mode:           PreviewMode,
	}
	report := InfrastructurePlanReport{
		ReportEnvelope: envelope,
		Summary: InfrastructurePlanSummary{
			reportKind: kind,
			Applyable:  document.Applyable,
			Complete:   document.Complete,
			Errored:    document.Errored,
		},
		ResourceChanges: make([]InfrastructureResourceChange, 0, len(document.ResourceChanges)),
		ResourceDrift:   make([]InfrastructureResourceChange, 0, len(document.ResourceDrift)),
	}

	for index, sourceChange := range document.ResourceChanges {
		change, err := projectInfrastructureResourceChange("resource_changes", index, sourceChange)
		if err != nil {
			return InfrastructurePlanReport{}, err
		}
		report.ResourceChanges = append(report.ResourceChanges, change)
		report.Summary.ResourceChanges.add(change.Action)
	}
	for index, sourceChange := range document.ResourceDrift {
		change, err := projectInfrastructureResourceChange("resource_drift", index, sourceChange)
		if err != nil {
			return InfrastructurePlanReport{}, err
		}
		report.ResourceDrift = append(report.ResourceDrift, change)
	}
	report.Summary.DriftedResources = len(report.ResourceDrift)

	outputNames := make([]string, 0, len(document.OutputChanges))
	for name := range document.OutputChanges {
		outputNames = append(outputNames, name)
	}
	slices.Sort(outputNames)
	for _, name := range outputNames {
		action, err := normalizeInfrastructureOutputAction(document.OutputChanges[name].Actions)
		if err != nil {
			return InfrastructurePlanReport{}, fmt.Errorf("output_changes[%q].actions: %w", name, err)
		}
		report.Summary.OutputChanges.add(action)
	}

	checkCount := 0
	for index, check := range document.Checks {
		if err := validateInfrastructureCheckStatus(
			fmt.Sprintf("checks[%d].status", index),
			check.Status,
		); err != nil {
			return InfrastructurePlanReport{}, err
		}
		if len(check.Instances) == 0 {
			checkCount++
			report.Summary.Checks.add(check.Status)
			if checkCount > MaximumInfrastructureCheckEntries {
				return InfrastructurePlanReport{}, fmt.Errorf(
					"checks must contain at most %d summarized entries",
					MaximumInfrastructureCheckEntries,
				)
			}
			continue
		}
		for instanceIndex, instance := range check.Instances {
			field := fmt.Sprintf("checks[%d].instances[%d].status", index, instanceIndex)
			if err := validateInfrastructureCheckStatus(field, instance.Status); err != nil {
				return InfrastructurePlanReport{}, err
			}
			checkCount++
			report.Summary.Checks.add(instance.Status)
			if checkCount > MaximumInfrastructureCheckEntries {
				return InfrastructurePlanReport{}, fmt.Errorf(
					"checks must contain at most %d summarized entries",
					MaximumInfrastructureCheckEntries,
				)
			}
		}
	}

	slices.SortFunc(report.ResourceChanges, compareInfrastructureResourceChanges)
	slices.SortFunc(report.ResourceDrift, compareInfrastructureResourceChanges)
	if _, err := Encode(report); err != nil {
		return InfrastructurePlanReport{}, err
	}

	return report, nil
}

func (r InfrastructurePlanReport) Metadata() ReportEnvelope {
	return r.ReportEnvelope
}

func (r InfrastructurePlanReport) ReportSummary() Summary {
	summary := r.Summary
	summary.reportKind = r.Kind

	return &summary
}

func (r InfrastructurePlanReport) Validate() error {
	if err := r.ReportEnvelope.Validate(); err != nil {
		return err
	}
	if len(r.ResourceChanges)+len(r.ResourceDrift) > MaximumInfrastructureResourceEntries {
		return fmt.Errorf(
			"resourceChanges and resourceDrift must contain at most %d entries in total",
			MaximumInfrastructureResourceEntries,
		)
	}

	calculated := InfrastructureActionCounts{}
	for index, change := range r.ResourceChanges {
		if err := change.validate(fmt.Sprintf("resourceChanges[%d]", index)); err != nil {
			return err
		}
		calculated.add(change.Action)
		if index > 0 && compareInfrastructureResourceChanges(r.ResourceChanges[index-1], change) > 0 {
			return errors.New("resourceChanges must use deterministic order")
		}
	}
	if calculated != r.Summary.ResourceChanges {
		return errors.New("summary resourceChanges does not match report detail")
	}
	for index, change := range r.ResourceDrift {
		if err := change.validate(fmt.Sprintf("resourceDrift[%d]", index)); err != nil {
			return err
		}
		if index > 0 && compareInfrastructureResourceChanges(r.ResourceDrift[index-1], change) > 0 {
			return errors.New("resourceDrift must use deterministic order")
		}
	}
	if r.Summary.DriftedResources != len(r.ResourceDrift) {
		return errors.New("summary driftedResources does not match report detail")
	}
	if err := r.Summary.Validate(); err != nil {
		return err
	}

	return nil
}

func (s InfrastructurePlanSummary) ReportKind() ReportKind {
	return s.reportKind
}

func (s InfrastructurePlanSummary) Validate() error {
	if err := s.ResourceChanges.validate("summary.resourceChanges", MaximumInfrastructureResourceEntries); err != nil {
		return err
	}
	if s.DriftedResources < 0 || s.ResourceChanges.Total+s.DriftedResources > MaximumInfrastructureResourceEntries {
		return fmt.Errorf(
			"summary resourceChanges and driftedResources must contain at most %d entries in total",
			MaximumInfrastructureResourceEntries,
		)
	}
	if err := s.OutputChanges.validate("summary.outputChanges", MaximumInfrastructureOutputEntries); err != nil {
		return err
	}
	if err := s.Checks.validate(); err != nil {
		return err
	}

	return nil
}

func (c InfrastructureResourceChange) validate(field string) error {
	for _, item := range []struct {
		name     string
		value    string
		maximum  int
		required bool
	}{
		{name: "address", value: c.Address, maximum: maximumResourceAddressLength, required: true},
		{name: "replacedObjectKey", value: c.ReplacedObjectKey, maximum: maximumResourceAddressLength},
		{name: "previousAddress", value: c.PreviousAddress, maximum: maximumResourceAddressLength},
		{name: "moduleAddress", value: c.ModuleAddress, maximum: maximumResourceAddressLength},
		{name: "mode", value: c.Mode, maximum: maximumResourceModeLength, required: true},
		{name: "type", value: c.Type, maximum: maximumResourceTypeLength, required: true},
		{name: "name", value: c.Name, maximum: maximumResourceNameLength, required: true},
		{name: "providerName", value: c.ProviderName, maximum: maximumProviderNameLength, required: true},
		{name: "actionReason", value: c.ActionReason, maximum: maximumResourceActionReasonLength},
	} {
		if item.required {
			if err := validateRequiredString(field+"."+item.name, item.value, item.maximum); err != nil {
				return err
			}
		} else if err := validateOptionalString(field+"."+item.name, item.value, item.maximum); err != nil {
			return err
		}
	}

	switch c.Action {
	case InfrastructureActionNoOp, InfrastructureActionCreate, InfrastructureActionRead, InfrastructureActionUpdate,
		InfrastructureActionDelete, InfrastructureActionForget:
		if c.ReplacementOrder != "" {
			return fmt.Errorf("%s.replacementOrder is only valid for replace actions", field)
		}
	case InfrastructureActionReplace:
		if c.ReplacementOrder != InfrastructureReplacementDestroyBeforeCreate &&
			c.ReplacementOrder != InfrastructureReplacementCreateBeforeDestroy {
			return fmt.Errorf("%s.replacementOrder is required for replace actions", field)
		}
	default:
		return fmt.Errorf("%s.action is not supported", field)
	}

	return nil
}

func projectInfrastructureResourceChange(
	collection string,
	index int,
	source infrastructureResourceChangeSource,
) (InfrastructureResourceChange, error) {
	action, order, err := normalizeInfrastructureAction(source.Change.Actions)
	if err != nil {
		return InfrastructureResourceChange{}, fmt.Errorf("%s[%d].change.actions: %w", collection, index, err)
	}

	change := InfrastructureResourceChange{
		Address:           source.Address,
		ReplacedObjectKey: source.Deposed,
		PreviousAddress:   source.PreviousAddress,
		ModuleAddress:     source.ModuleAddress,
		Mode:              source.Mode,
		Type:              source.Type,
		Name:              source.Name,
		ProviderName:      source.ProviderName,
		Action:            action,
		ReplacementOrder:  order,
		ActionReason:      source.ActionReason,
	}
	if err := change.validate(fmt.Sprintf("%s[%d]", collection, index)); err != nil {
		return InfrastructureResourceChange{}, err
	}

	return change, nil
}

func normalizeInfrastructureAction(
	actions []string,
) (InfrastructureAction, InfrastructureReplacementOrder, error) {
	if len(actions) == 1 {
		switch actions[0] {
		case string(InfrastructureActionNoOp):
			return InfrastructureActionNoOp, "", nil
		case string(InfrastructureActionCreate):
			return InfrastructureActionCreate, "", nil
		case string(InfrastructureActionRead):
			return InfrastructureActionRead, "", nil
		case string(InfrastructureActionUpdate):
			return InfrastructureActionUpdate, "", nil
		case string(InfrastructureActionDelete):
			return InfrastructureActionDelete, "", nil
		case string(InfrastructureActionForget):
			return InfrastructureActionForget, "", nil
		}
	}
	if len(actions) == 2 {
		switch {
		case actions[0] == "delete" && actions[1] == "create":
			return InfrastructureActionReplace, InfrastructureReplacementDestroyBeforeCreate, nil
		case actions[0] == "create" && actions[1] == "delete":
			return InfrastructureActionReplace, InfrastructureReplacementCreateBeforeDestroy, nil
		}
	}

	return "", "", errors.New("action combination is not supported")
}

func normalizeInfrastructureOutputAction(actions []string) (InfrastructureAction, error) {
	if len(actions) == 1 {
		switch actions[0] {
		case string(InfrastructureActionNoOp):
			return InfrastructureActionNoOp, nil
		case string(InfrastructureActionCreate):
			return InfrastructureActionCreate, nil
		case string(InfrastructureActionUpdate):
			return InfrastructureActionUpdate, nil
		case string(InfrastructureActionDelete):
			return InfrastructureActionDelete, nil
		}
	}

	return "", errors.New("output action combination is not supported")
}

func validateInfrastructureFormatVersion(value string) error {
	if err := validateRequiredString("format_version", value, 32); err != nil {
		return err
	}
	majorText, minorText, found := strings.Cut(value, ".")
	if !found || majorText == "" || minorText == "" || strings.Contains(minorText, ".") {
		return errors.New("format_version must contain a numeric major and minor version")
	}
	major, err := strconv.ParseUint(majorText, 10, 32)
	if err != nil {
		return errors.New("format_version must contain a numeric major and minor version")
	}
	if _, err := strconv.ParseUint(minorText, 10, 32); err != nil {
		return errors.New("format_version must contain a numeric major and minor version")
	}
	if major != 1 {
		return fmt.Errorf("format_version major %d is not supported", major)
	}

	return nil
}

func validateInfrastructureCheckStatus(field string, status string) error {
	switch status {
	case "pass", "fail", "error", "unknown":
		return nil
	default:
		return fmt.Errorf("%s is not a supported infrastructure plan check status", field)
	}
}

func validateRequiredString(field string, value string, maximum int) error {
	if strings.TrimSpace(value) == "" {
		return fmt.Errorf("%s is required", field)
	}

	return validateOptionalString(field, value, maximum)
}

func validateOptionalString(field string, value string, maximum int) error {
	if utf8.RuneCountInString(value) > maximum {
		return fmt.Errorf("%s must not exceed %d characters", field, maximum)
	}

	return nil
}

func (c *InfrastructureActionCounts) add(action InfrastructureAction) {
	c.Total++
	switch action {
	case InfrastructureActionNoOp:
		c.NoOp++
	case InfrastructureActionCreate:
		c.Create++
	case InfrastructureActionRead:
		c.Read++
	case InfrastructureActionUpdate:
		c.Update++
	case InfrastructureActionDelete:
		c.Delete++
	case InfrastructureActionForget:
		c.Forget++
	case InfrastructureActionReplace:
		c.Replace++
	}
}

func (c InfrastructureActionCounts) validate(field string, maximum int) error {
	if c.Total < 0 || c.NoOp < 0 || c.Create < 0 || c.Read < 0 || c.Update < 0 || c.Delete < 0 ||
		c.Forget < 0 || c.Replace < 0 {
		return fmt.Errorf("%s counts must not be negative", field)
	}
	if c.Total != c.NoOp+c.Create+c.Read+c.Update+c.Delete+c.Forget+c.Replace {
		return fmt.Errorf("%s total does not match its action counts", field)
	}
	if c.Total > maximum {
		return fmt.Errorf("%s must not exceed %d entries", field, maximum)
	}

	return nil
}

func (c *InfrastructureCheckCounts) add(status string) {
	c.Total++
	switch status {
	case "pass":
		c.Pass++
	case "fail":
		c.Fail++
	case "error":
		c.Error++
	case "unknown":
		c.Unknown++
	}
}

func (c InfrastructureCheckCounts) validate() error {
	if c.Total < 0 || c.Pass < 0 || c.Fail < 0 || c.Error < 0 || c.Unknown < 0 {
		return errors.New("summary.checks counts must not be negative")
	}
	if c.Total != c.Pass+c.Fail+c.Error+c.Unknown {
		return errors.New("summary.checks total does not match its status counts")
	}
	if c.Total > MaximumInfrastructureCheckEntries {
		return fmt.Errorf("summary.checks must not exceed %d entries", MaximumInfrastructureCheckEntries)
	}

	return nil
}

func compareInfrastructureResourceChanges(left, right InfrastructureResourceChange) int {
	return cmp.Or(
		cmp.Compare(left.Address, right.Address),
		cmp.Compare(left.ReplacedObjectKey, right.ReplacedObjectKey),
		cmp.Compare(left.PreviousAddress, right.PreviousAddress),
		cmp.Compare(left.ModuleAddress, right.ModuleAddress),
		cmp.Compare(left.Mode, right.Mode),
		cmp.Compare(left.Type, right.Type),
		cmp.Compare(left.Name, right.Name),
		cmp.Compare(left.ProviderName, right.ProviderName),
		cmp.Compare(left.Action, right.Action),
		cmp.Compare(left.ReplacementOrder, right.ReplacementOrder),
		cmp.Compare(left.ActionReason, right.ActionReason),
	)
}
