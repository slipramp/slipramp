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
	MaximumTerraformResourceEntries = 20_000
	MaximumTerraformOutputEntries   = 10_000
	MaximumTerraformCheckEntries    = 10_000

	maximumTerraformVersionLength     = 128
	maximumResourceAddressLength      = 2_048
	maximumResourceModeLength         = 64
	maximumResourceTypeLength         = 256
	maximumResourceNameLength         = 256
	maximumProviderNameLength         = 512
	maximumResourceActionReasonLength = 256
)

type TerraformAction string

const (
	TerraformActionNoOp    TerraformAction = "no-op"
	TerraformActionCreate  TerraformAction = "create"
	TerraformActionRead    TerraformAction = "read"
	TerraformActionUpdate  TerraformAction = "update"
	TerraformActionDelete  TerraformAction = "delete"
	TerraformActionReplace TerraformAction = "replace"
)

type TerraformReplacementOrder string

const (
	TerraformReplacementDestroyBeforeCreate TerraformReplacementOrder = "destroy-before-create"
	TerraformReplacementCreateBeforeDestroy TerraformReplacementOrder = "create-before-destroy"
)

type TerraformPlanReport struct {
	ReportEnvelope
	Summary         TerraformPlanSummary      `json:"summary"`
	ResourceChanges []TerraformResourceChange `json:"resourceChanges"`
	ResourceDrift   []TerraformResourceChange `json:"resourceDrift"`
}

type TerraformPlanSummary struct {
	ResourceChanges  TerraformActionCounts `json:"resourceChanges"`
	DriftedResources int                   `json:"driftedResources"`
	Checks           TerraformCheckCounts  `json:"checks"`
	OutputChanges    TerraformActionCounts `json:"outputChanges"`
	Applyable        *bool                 `json:"applyable,omitempty"`
	Complete         *bool                 `json:"complete,omitempty"`
	Errored          *bool                 `json:"errored,omitempty"`
}

type TerraformActionCounts struct {
	Total   int `json:"total"`
	NoOp    int `json:"noOp"`
	Create  int `json:"create"`
	Read    int `json:"read"`
	Update  int `json:"update"`
	Delete  int `json:"delete"`
	Replace int `json:"replace"`
}

type TerraformCheckCounts struct {
	Total   int `json:"total"`
	Pass    int `json:"pass"`
	Fail    int `json:"fail"`
	Error   int `json:"error"`
	Unknown int `json:"unknown"`
}

type TerraformResourceChange struct {
	Address           string                    `json:"address"`
	ReplacedObjectKey string                    `json:"replacedObjectKey,omitempty"`
	PreviousAddress   string                    `json:"previousAddress,omitempty"`
	ModuleAddress     string                    `json:"moduleAddress,omitempty"`
	Mode              string                    `json:"mode"`
	Type              string                    `json:"type"`
	Name              string                    `json:"name"`
	ProviderName      string                    `json:"providerName"`
	Action            TerraformAction           `json:"action"`
	ReplacementOrder  TerraformReplacementOrder `json:"replacementOrder,omitempty"`
	ActionReason      string                    `json:"actionReason,omitempty"`
}

type terraformPlanAdapter struct{}

func (terraformPlanAdapter) Kind() ReportKind {
	return TerraformPlanKind
}

func (terraformPlanAdapter) MaximumSourceSizeBytes() int64 {
	return MaximumTerraformPlanSourceSizeBytes
}

func (terraformPlanAdapter) Decode(source io.Reader) (Report, error) {
	return DecodeTerraformPlan(source)
}

func (terraformPlanAdapter) newReport() Report {
	return &TerraformPlanReport{}
}

func (terraformPlanAdapter) newSummary() Summary {
	return &TerraformPlanSummary{}
}

func (terraformPlanAdapter) validateEnvelope(envelope ReportEnvelope) error {
	if envelope.SchemaVersion != TerraformPlanSchemaVersion ||
		envelope.AdapterVersion != TerraformPlanAdapterVersion ||
		envelope.Tool != "terraform" || envelope.Mode != PreviewMode {
		return errors.New("terraform plan report envelope is invalid")
	}

	return validateOptionalString("toolVersion", envelope.ToolVersion, maximumTerraformVersionLength)
}

type terraformPlanSource struct {
	FormatVersion    string                           `json:"format_version"`
	TerraformVersion string                           `json:"terraform_version"`
	Applyable        *bool                            `json:"applyable"`
	Complete         *bool                            `json:"complete"`
	Errored          *bool                            `json:"errored"`
	ResourceChanges  []terraformResourceChangeSource  `json:"resource_changes"`
	ResourceDrift    []terraformResourceChangeSource  `json:"resource_drift"`
	OutputChanges    map[string]terraformChangeSource `json:"output_changes"`
	Checks           []terraformCheckSource           `json:"checks"`
}

type terraformResourceChangeSource struct {
	Address         string                `json:"address"`
	Deposed         string                `json:"deposed"`
	PreviousAddress string                `json:"previous_address"`
	ModuleAddress   string                `json:"module_address"`
	Mode            string                `json:"mode"`
	Type            string                `json:"type"`
	Name            string                `json:"name"`
	ProviderName    string                `json:"provider_name"`
	Change          terraformChangeSource `json:"change"`
	ActionReason    string                `json:"action_reason"`
}

type terraformChangeSource struct {
	Actions []string `json:"actions"`
}

type terraformCheckSource struct {
	Status    string                         `json:"status"`
	Instances []terraformCheckInstanceSource `json:"instances"`
}

type terraformCheckInstanceSource struct {
	Status string `json:"status"`
}

func DecodeTerraformPlan(source io.Reader) (TerraformPlanReport, error) {
	if source == nil {
		return TerraformPlanReport{}, errors.New("terraform plan source is required")
	}

	contents, err := io.ReadAll(io.LimitReader(source, MaximumTerraformPlanSourceSizeBytes+1))
	if err != nil {
		return TerraformPlanReport{}, fmt.Errorf("%w: %w", ErrSourceRead, err)
	}
	if int64(len(contents)) > MaximumTerraformPlanSourceSizeBytes {
		return TerraformPlanReport{}, fmt.Errorf(
			"%w: Terraform plan source must not exceed %d bytes",
			ErrSourceTooLarge,
			MaximumTerraformPlanSourceSizeBytes,
		)
	}
	if !utf8.Valid(contents) {
		return TerraformPlanReport{}, errors.New("terraform plan source must be valid UTF-8 JSON")
	}

	decoder := json.NewDecoder(bytes.NewReader(contents))
	var document terraformPlanSource
	if err := decoder.Decode(&document); err != nil {
		return TerraformPlanReport{}, fmt.Errorf("decode Terraform plan source: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return TerraformPlanReport{}, errors.New("unexpected data after Terraform plan JSON object")
		}

		return TerraformPlanReport{}, fmt.Errorf("decode trailing Terraform plan data: %w", err)
	}

	if err := validateTerraformFormatVersion(document.FormatVersion); err != nil {
		return TerraformPlanReport{}, err
	}
	if document.ResourceChanges == nil && document.ResourceDrift == nil && document.OutputChanges == nil &&
		document.Checks == nil && document.Applyable == nil && document.Complete == nil && document.Errored == nil {
		return TerraformPlanReport{}, errors.New("terraform plan source does not contain a plan representation")
	}
	if err := validateOptionalString(
		"terraform_version",
		document.TerraformVersion,
		maximumTerraformVersionLength,
	); err != nil {
		return TerraformPlanReport{}, err
	}
	if len(document.ResourceChanges)+len(document.ResourceDrift) > MaximumTerraformResourceEntries {
		return TerraformPlanReport{}, fmt.Errorf(
			"resource_changes and resource_drift must contain at most %d entries in total",
			MaximumTerraformResourceEntries,
		)
	}
	if len(document.OutputChanges) > MaximumTerraformOutputEntries {
		return TerraformPlanReport{}, fmt.Errorf(
			"output_changes must contain at most %d entries",
			MaximumTerraformOutputEntries,
		)
	}

	envelope := ReportEnvelope{
		Kind:           TerraformPlanKind,
		SchemaVersion:  TerraformPlanSchemaVersion,
		AdapterVersion: TerraformPlanAdapterVersion,
		Tool:           "terraform",
		ToolVersion:    document.TerraformVersion,
		Mode:           PreviewMode,
	}
	report := TerraformPlanReport{
		ReportEnvelope: envelope,
		Summary: TerraformPlanSummary{
			Applyable: document.Applyable,
			Complete:  document.Complete,
			Errored:   document.Errored,
		},
		ResourceChanges: make([]TerraformResourceChange, 0, len(document.ResourceChanges)),
		ResourceDrift:   make([]TerraformResourceChange, 0, len(document.ResourceDrift)),
	}

	for index, sourceChange := range document.ResourceChanges {
		change, err := projectTerraformResourceChange("resource_changes", index, sourceChange)
		if err != nil {
			return TerraformPlanReport{}, err
		}
		report.ResourceChanges = append(report.ResourceChanges, change)
		report.Summary.ResourceChanges.add(change.Action)
	}
	for index, sourceChange := range document.ResourceDrift {
		change, err := projectTerraformResourceChange("resource_drift", index, sourceChange)
		if err != nil {
			return TerraformPlanReport{}, err
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
		action, err := normalizeTerraformOutputAction(document.OutputChanges[name].Actions)
		if err != nil {
			return TerraformPlanReport{}, fmt.Errorf("output_changes[%q].actions: %w", name, err)
		}
		report.Summary.OutputChanges.add(action)
	}

	checkCount := 0
	for index, check := range document.Checks {
		if err := validateTerraformCheckStatus(
			fmt.Sprintf("checks[%d].status", index),
			check.Status,
		); err != nil {
			return TerraformPlanReport{}, err
		}
		if len(check.Instances) == 0 {
			checkCount++
			report.Summary.Checks.add(check.Status)
			if checkCount > MaximumTerraformCheckEntries {
				return TerraformPlanReport{}, fmt.Errorf(
					"checks must contain at most %d summarized entries",
					MaximumTerraformCheckEntries,
				)
			}
			continue
		}
		for instanceIndex, instance := range check.Instances {
			field := fmt.Sprintf("checks[%d].instances[%d].status", index, instanceIndex)
			if err := validateTerraformCheckStatus(field, instance.Status); err != nil {
				return TerraformPlanReport{}, err
			}
			checkCount++
			report.Summary.Checks.add(instance.Status)
			if checkCount > MaximumTerraformCheckEntries {
				return TerraformPlanReport{}, fmt.Errorf(
					"checks must contain at most %d summarized entries",
					MaximumTerraformCheckEntries,
				)
			}
		}
	}

	slices.SortFunc(report.ResourceChanges, compareTerraformResourceChanges)
	slices.SortFunc(report.ResourceDrift, compareTerraformResourceChanges)
	if _, err := Encode(report); err != nil {
		return TerraformPlanReport{}, err
	}

	return report, nil
}

func (r TerraformPlanReport) Metadata() ReportEnvelope {
	return r.ReportEnvelope
}

func (r TerraformPlanReport) ReportSummary() Summary {
	return &r.Summary
}

func (r TerraformPlanReport) Validate() error {
	if err := r.ReportEnvelope.Validate(); err != nil {
		return err
	}
	if len(r.ResourceChanges)+len(r.ResourceDrift) > MaximumTerraformResourceEntries {
		return fmt.Errorf(
			"resourceChanges and resourceDrift must contain at most %d entries in total",
			MaximumTerraformResourceEntries,
		)
	}

	calculated := TerraformActionCounts{}
	for index, change := range r.ResourceChanges {
		if err := change.validate(fmt.Sprintf("resourceChanges[%d]", index)); err != nil {
			return err
		}
		calculated.add(change.Action)
		if index > 0 && compareTerraformResourceChanges(r.ResourceChanges[index-1], change) > 0 {
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
		if index > 0 && compareTerraformResourceChanges(r.ResourceDrift[index-1], change) > 0 {
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

func (s TerraformPlanSummary) ReportKind() ReportKind {
	return TerraformPlanKind
}

func (s TerraformPlanSummary) Validate() error {
	if err := s.ResourceChanges.validate("summary.resourceChanges", MaximumTerraformResourceEntries); err != nil {
		return err
	}
	if s.DriftedResources < 0 || s.ResourceChanges.Total+s.DriftedResources > MaximumTerraformResourceEntries {
		return fmt.Errorf(
			"summary resourceChanges and driftedResources must contain at most %d entries in total",
			MaximumTerraformResourceEntries,
		)
	}
	if err := s.OutputChanges.validate("summary.outputChanges", MaximumTerraformOutputEntries); err != nil {
		return err
	}
	if err := s.Checks.validate(); err != nil {
		return err
	}

	return nil
}

func (c TerraformResourceChange) validate(field string) error {
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
	case TerraformActionNoOp, TerraformActionCreate, TerraformActionRead, TerraformActionUpdate,
		TerraformActionDelete:
		if c.ReplacementOrder != "" {
			return fmt.Errorf("%s.replacementOrder is only valid for replace actions", field)
		}
	case TerraformActionReplace:
		if c.ReplacementOrder != TerraformReplacementDestroyBeforeCreate &&
			c.ReplacementOrder != TerraformReplacementCreateBeforeDestroy {
			return fmt.Errorf("%s.replacementOrder is required for replace actions", field)
		}
	default:
		return fmt.Errorf("%s.action is not supported", field)
	}

	return nil
}

func projectTerraformResourceChange(
	collection string,
	index int,
	source terraformResourceChangeSource,
) (TerraformResourceChange, error) {
	action, order, err := normalizeTerraformAction(source.Change.Actions)
	if err != nil {
		return TerraformResourceChange{}, fmt.Errorf("%s[%d].change.actions: %w", collection, index, err)
	}

	change := TerraformResourceChange{
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
		return TerraformResourceChange{}, err
	}

	return change, nil
}

func normalizeTerraformAction(actions []string) (TerraformAction, TerraformReplacementOrder, error) {
	if len(actions) == 1 {
		switch actions[0] {
		case string(TerraformActionNoOp):
			return TerraformActionNoOp, "", nil
		case string(TerraformActionCreate):
			return TerraformActionCreate, "", nil
		case string(TerraformActionRead):
			return TerraformActionRead, "", nil
		case string(TerraformActionUpdate):
			return TerraformActionUpdate, "", nil
		case string(TerraformActionDelete):
			return TerraformActionDelete, "", nil
		}
	}
	if len(actions) == 2 {
		switch {
		case actions[0] == "delete" && actions[1] == "create":
			return TerraformActionReplace, TerraformReplacementDestroyBeforeCreate, nil
		case actions[0] == "create" && actions[1] == "delete":
			return TerraformActionReplace, TerraformReplacementCreateBeforeDestroy, nil
		}
	}

	return "", "", errors.New("action combination is not supported")
}

func normalizeTerraformOutputAction(actions []string) (TerraformAction, error) {
	if len(actions) == 1 {
		switch actions[0] {
		case string(TerraformActionNoOp):
			return TerraformActionNoOp, nil
		case string(TerraformActionCreate):
			return TerraformActionCreate, nil
		case string(TerraformActionUpdate):
			return TerraformActionUpdate, nil
		case string(TerraformActionDelete):
			return TerraformActionDelete, nil
		}
	}

	return "", errors.New("output action combination is not supported")
}

func validateTerraformFormatVersion(value string) error {
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

func validateTerraformCheckStatus(field string, status string) error {
	switch status {
	case "pass", "fail", "error", "unknown":
		return nil
	default:
		return fmt.Errorf("%s is not a supported Terraform check status", field)
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

func (c *TerraformActionCounts) add(action TerraformAction) {
	c.Total++
	switch action {
	case TerraformActionNoOp:
		c.NoOp++
	case TerraformActionCreate:
		c.Create++
	case TerraformActionRead:
		c.Read++
	case TerraformActionUpdate:
		c.Update++
	case TerraformActionDelete:
		c.Delete++
	case TerraformActionReplace:
		c.Replace++
	}
}

func (c TerraformActionCounts) validate(field string, maximum int) error {
	if c.Total < 0 || c.NoOp < 0 || c.Create < 0 || c.Read < 0 || c.Update < 0 || c.Delete < 0 ||
		c.Replace < 0 {
		return fmt.Errorf("%s counts must not be negative", field)
	}
	if c.Total != c.NoOp+c.Create+c.Read+c.Update+c.Delete+c.Replace {
		return fmt.Errorf("%s total does not match its action counts", field)
	}
	if c.Total > maximum {
		return fmt.Errorf("%s must not exceed %d entries", field, maximum)
	}

	return nil
}

func (c *TerraformCheckCounts) add(status string) {
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

func (c TerraformCheckCounts) validate() error {
	if c.Total < 0 || c.Pass < 0 || c.Fail < 0 || c.Error < 0 || c.Unknown < 0 {
		return errors.New("summary.checks counts must not be negative")
	}
	if c.Total != c.Pass+c.Fail+c.Error+c.Unknown {
		return errors.New("summary.checks total does not match its status counts")
	}
	if c.Total > MaximumTerraformCheckEntries {
		return fmt.Errorf("summary.checks must not exceed %d entries", MaximumTerraformCheckEntries)
	}

	return nil
}

func compareTerraformResourceChanges(left, right TerraformResourceChange) int {
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
