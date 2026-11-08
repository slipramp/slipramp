package ansible

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	ansiblesource "github.com/slipramp/slipramp/internal/runreport/ansible/source"
	"github.com/slipramp/slipramp/internal/runreport/model"
)

type ansibleEventDataSource struct {
	UUID         string          `json:"uuid"`
	Name         string          `json:"name"`
	Play         string          `json:"play"`
	PlayUUID     string          `json:"play_uuid"`
	Task         string          `json:"task"`
	TaskUUID     string          `json:"task_uuid"`
	TaskAction   string          `json:"task_action"`
	TaskPath     string          `json:"task_path"`
	Host         string          `json:"host"`
	IgnoreErrors bool            `json:"ignore_errors"`
	Res          json.RawMessage `json:"res"`
}

type ansibleResultSource struct {
	Changed  bool            `json:"changed"`
	Censored json.RawMessage `json:"censored"`
}

type ansibleStatsSource struct {
	Changed   map[string]int `json:"changed"`
	Dark      map[string]int `json:"dark"`
	Failures  map[string]int `json:"failures"`
	Ignored   map[string]int `json:"ignored"`
	OK        map[string]int `json:"ok"`
	Processed map[string]int `json:"processed"`
	Rescued   map[string]int `json:"rescued"`
	Skipped   map[string]int `json:"skipped"`
}

func DecodeAnsiblePlaybook(source io.Reader) (AnsiblePlaybookReport, error) {
	if source == nil {
		return AnsiblePlaybookReport{}, errors.New("ansible playbook source is required")
	}

	contents, err := io.ReadAll(io.LimitReader(source, ansiblesource.MaximumSourceSize+1))
	if err != nil {
		return AnsiblePlaybookReport{}, fmt.Errorf("%w: %w", model.ErrSourceRead, err)
	}
	if int64(len(contents)) > ansiblesource.MaximumSourceSize {
		return AnsiblePlaybookReport{}, fmt.Errorf(
			"%w: Ansible playbook source must not exceed %d bytes",
			model.ErrSourceTooLarge,
			ansiblesource.MaximumSourceSize,
		)
	}
	if !utf8.Valid(contents) {
		return AnsiblePlaybookReport{}, errors.New("ansible playbook source must be valid UTF-8 JSON")
	}

	decoder := json.NewDecoder(bytes.NewReader(contents))
	var document ansiblesource.Document
	if err := decoder.Decode(&document); err != nil {
		return AnsiblePlaybookReport{}, fmt.Errorf("decode Ansible playbook source: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return AnsiblePlaybookReport{}, errors.New("unexpected data after Ansible playbook JSON object")
		}

		return AnsiblePlaybookReport{}, fmt.Errorf("decode trailing Ansible playbook data: %w", err)
	}
	if err := validateAnsibleSourceEnvelope(document); err != nil {
		return AnsiblePlaybookReport{}, err
	}
	if len(document.Events) > ansiblesource.MaximumSourceEvents {
		return AnsiblePlaybookReport{}, fmt.Errorf(
			"events must contain at most %d entries",
			ansiblesource.MaximumSourceEvents,
		)
	}

	envelope := model.Envelope{
		Kind:           PlaybookKind,
		SchemaVersion:  SchemaVersion,
		AdapterVersion: AdapterVersion,
		Tool:           "ansible",
		ToolVersion:    document.Versions.AnsibleCore,
		Mode:           model.Mode(document.Mode),
	}
	report := AnsiblePlaybookReport{
		Envelope: envelope,
		Runner: AnsibleRunnerSummary{
			Version:  document.Versions.AnsibleRunner,
			Status:   AnsibleRunnerStatus(document.Runner.Status),
			ExitCode: document.Runner.ExitCode,
		},
		Plays: []AnsiblePlay{},
	}
	builder := ansibleReportBuilder{
		report:              &report,
		repositoryDirectory: document.RepositoryDirectory,
		playIndexes:         make(map[string]int),
		taskIndexes:         make(map[string]ansibleTaskIndex),
		hosts:               make(map[string]struct{}),
	}

	previousCounter := 0
	for index, encodedEvent := range document.Events {
		var event ansiblesource.Event
		if err := json.Unmarshal(encodedEvent, &event); err != nil {
			return AnsiblePlaybookReport{}, fmt.Errorf("decode events[%d]: %w", index, err)
		}
		if event.Counter <= 0 {
			return AnsiblePlaybookReport{}, fmt.Errorf("events[%d].counter must be positive", index)
		}
		if event.Counter <= previousCounter {
			return AnsiblePlaybookReport{}, errors.New("events must use strictly increasing counters")
		}
		previousCounter = event.Counter
		if err := validateAnsibleRequiredText(
			fmt.Sprintf("events[%d].uuid", index),
			event.UUID,
			maximumAnsibleIdentifierLength,
		); err != nil {
			return AnsiblePlaybookReport{}, err
		}
		if err := validateAnsibleRequiredText(
			fmt.Sprintf("events[%d].event", index),
			event.Event,
			maximumAnsibleEventNameLength,
		); err != nil {
			return AnsiblePlaybookReport{}, err
		}
		if err := builder.consume(index, event); err != nil {
			return AnsiblePlaybookReport{}, err
		}
	}

	if err := builder.finish(); err != nil {
		return AnsiblePlaybookReport{}, err
	}
	if _, err := model.Encode(report); err != nil {
		return AnsiblePlaybookReport{}, err
	}

	return report, nil
}

func validateAnsibleSourceEnvelope(source ansiblesource.Document) error {
	if source.SchemaVersion != ansiblesource.SourceSchemaVersion {
		return fmt.Errorf("ansible playbook source schema version %d is not supported", source.SchemaVersion)
	}
	if source.Mode != ansiblesource.ModePreview && source.Mode != ansiblesource.ModeApply {
		return errors.New("ansible playbook source mode must be preview or apply")
	}
	if err := validateAnsibleRequiredText(
		"repositoryDirectory", source.RepositoryDirectory, maximumAnsibleRawPathLength,
	); err != nil {
		return err
	}
	if !path.IsAbs(source.RepositoryDirectory) || path.Clean(source.RepositoryDirectory) != source.RepositoryDirectory {
		return errors.New("repositoryDirectory must be an absolute clean path")
	}
	if source.Versions == nil {
		return errors.New("ansible playbook source versions are required")
	}
	if err := validateAnsibleOptionalText(
		"versions.ansibleCore",
		source.Versions.AnsibleCore,
		maximumAnsibleVersionLength,
	); err != nil {
		return err
	}
	if err := validateAnsibleOptionalText(
		"versions.ansibleRunner",
		source.Versions.AnsibleRunner,
		maximumAnsibleVersionLength,
	); err != nil {
		return err
	}
	if source.Runner == nil || !source.Runner.HasExitCode() {
		return errors.New("ansible playbook source runner status and exitCode are required")
	}
	if !validAnsibleRunnerStatus(AnsibleRunnerStatus(source.Runner.Status)) {
		return errors.New("ansible playbook source runner status is not supported")
	}
	if source.Runner.ExitCode < 0 || source.Runner.ExitCode > 255 {
		return errors.New("ansible playbook source runner exitCode must be between 0 and 255")
	}
	if source.Events == nil {
		return errors.New("ansible playbook source events are required")
	}

	return nil
}

type ansibleTaskIndex struct {
	play int
	task int
}

type ansibleReportBuilder struct {
	report              *AnsiblePlaybookReport
	repositoryDirectory string
	playIndexes         map[string]int
	taskIndexes         map[string]ansibleTaskIndex
	hosts               map[string]struct{}
	stats               *ansibleStatsSource
	results             AnsibleOutcomeCounts
	tasks               int
}

func (b *ansibleReportBuilder) consume(index int, event ansiblesource.Event) error {
	switch event.Event {
	case "playbook_on_play_start":
		var data ansibleEventDataSource
		if err := decodeAnsibleEventData(index, event.EventData, &data); err != nil {
			return err
		}
		_, err := b.ensurePlay(event.Counter, firstNonempty(data.UUID, data.PlayUUID), firstNonempty(data.Name, data.Play))

		return err
	case "playbook_on_task_start":
		var data ansibleEventDataSource
		if err := decodeAnsibleEventData(index, event.EventData, &data); err != nil {
			return err
		}
		_, err := b.ensureTask(event.Counter, data)

		return err
	case "runner_on_ok", "runner_on_failed", "runner_on_unreachable", "runner_on_skipped",
		"runner_item_on_ok", "runner_item_on_failed", "runner_item_on_skipped",
		"runner_on_async_ok", "runner_on_async_failed":
		return b.consumeResult(index, event)
	case "playbook_on_stats":
		if b.stats != nil {
			return errors.New("ansible playbook source contains multiple playbook_on_stats events")
		}
		var stats ansibleStatsSource
		if err := decodeAnsibleEventData(index, event.EventData, &stats); err != nil {
			return err
		}
		b.stats = &stats
	}

	return nil
}

func (b *ansibleReportBuilder) consumeResult(index int, event ansiblesource.Event) error {
	var data ansibleEventDataSource
	if err := decodeAnsibleEventData(index, event.EventData, &data); err != nil {
		return err
	}
	taskIndex, err := b.ensureTask(event.Counter, data)
	if err != nil {
		return err
	}
	if err := validateAnsibleRequiredText(
		fmt.Sprintf("events[%d].event_data.host", index),
		data.Host,
		maximumAnsibleHostNameLength,
	); err != nil {
		return err
	}

	resultSource, err := decodeAnsibleResult(index, data.Res, event.Event)
	if err != nil {
		return err
	}
	noLog := resultSource.Censored != nil
	outcome, err := ansibleEventOutcome(event.Event, resultSource.Changed)
	if err != nil {
		return err
	}
	ignored := (event.Event == "runner_on_failed" || event.Event == "runner_item_on_failed") && data.IgnoreErrors
	result := AnsibleTaskResult{
		Order:          event.Counter,
		Host:           data.Host,
		Outcome:        outcome,
		LoopItem:       strings.HasPrefix(event.Event, "runner_item_on_"),
		IgnoredFailure: ignored,
	}

	if b.results.Total >= MaximumAnsiblePlaybookResults {
		return fmt.Errorf("report must contain at most %d host-task results", MaximumAnsiblePlaybookResults)
	}
	b.hosts[result.Host] = struct{}{}
	if len(b.hosts) > MaximumAnsiblePlaybookHosts {
		return fmt.Errorf("report must contain at most %d hosts", MaximumAnsiblePlaybookHosts)
	}
	task := &b.report.Plays[taskIndex.play].Tasks[taskIndex.task]
	if noLog {
		redactAnsibleTask(task)
	}
	task.Results = append(task.Results, result)
	b.results.add(outcome)

	return nil
}

func decodeAnsibleEventData(index int, encoded json.RawMessage, destination any) error {
	if encoded == nil {
		return fmt.Errorf("events[%d].event_data is required", index)
	}
	if err := json.Unmarshal(encoded, destination); err != nil {
		return fmt.Errorf("decode events[%d].event_data: %w", index, err)
	}

	return nil
}

func decodeAnsibleResult(index int, encoded json.RawMessage, event string) (ansibleResultSource, error) {
	if encoded == nil || bytes.Equal(bytes.TrimSpace(encoded), []byte("null")) {
		// Runner omits res for ordinary skips; loop-item skips can carry censored results.
		if event == "runner_on_skipped" || event == "runner_item_on_skipped" {
			return ansibleResultSource{}, nil
		}
		return ansibleResultSource{}, fmt.Errorf("events[%d].event_data.res is required", index)
	}

	var result ansibleResultSource
	if err := json.Unmarshal(encoded, &result); err != nil {
		return ansibleResultSource{}, fmt.Errorf("decode events[%d].event_data.res: %w", index, err)
	}

	return result, nil
}

func (b *ansibleReportBuilder) ensurePlay(order int, id string, name string) (int, error) {
	if err := validateAnsibleRequiredText("play identity", id, maximumAnsibleIdentifierLength); err != nil {
		return 0, err
	}
	if playIndex, found := b.playIndexes[id]; found {
		if name != "" {
			if err := validateAnsibleRequiredText("play name", name, maximumAnsibleNameLength); err != nil {
				return 0, err
			}
			if b.report.Plays[playIndex].Name != name {
				return 0, fmt.Errorf("play %q has inconsistent presentation", id)
			}
		}

		return playIndex, nil
	}
	if err := validateAnsibleRequiredText("play name", name, maximumAnsibleNameLength); err != nil {
		return 0, err
	}
	if len(b.report.Plays) >= MaximumAnsiblePlaybookPlays {
		return 0, fmt.Errorf("report must contain at most %d plays", MaximumAnsiblePlaybookPlays)
	}

	playIndex := len(b.report.Plays)
	b.report.Plays = append(b.report.Plays, AnsiblePlay{
		Order: order,
		ID:    id,
		Name:  name,
		Tasks: []AnsibleTask{},
	})
	b.playIndexes[id] = playIndex

	return playIndex, nil
}

func (b *ansibleReportBuilder) ensureTask(order int, data ansibleEventDataSource) (ansibleTaskIndex, error) {
	playIndex, err := b.ensurePlay(order, data.PlayUUID, data.Play)
	if err != nil {
		return ansibleTaskIndex{}, err
	}
	if err := validateAnsibleRequiredText("task identity", data.TaskUUID, maximumAnsibleIdentifierLength); err != nil {
		return ansibleTaskIndex{}, err
	}
	if location, found := b.taskIndexes[data.TaskUUID]; found {
		if location.play != playIndex {
			return ansibleTaskIndex{}, fmt.Errorf("task %q appears in multiple plays", data.TaskUUID)
		}
		task := &b.report.Plays[location.play].Tasks[location.task]
		if task.Redacted {
			return location, nil
		}
		if err := mergeAnsibleTaskPresentation(task, data, b.repositoryDirectory); err != nil {
			return ansibleTaskIndex{}, err
		}

		return location, nil
	}
	if b.tasks >= MaximumAnsiblePlaybookTasks {
		return ansibleTaskIndex{}, fmt.Errorf("report must contain at most %d tasks", MaximumAnsiblePlaybookTasks)
	}

	name := firstNonempty(data.Task, data.Name)
	if err := validateAnsibleRequiredText("task name", name, maximumAnsibleNameLength); err != nil {
		return ansibleTaskIndex{}, err
	}
	if err := validateAnsibleRequiredText("task action", data.TaskAction, maximumAnsibleActionLength); err != nil {
		return ansibleTaskIndex{}, err
	}
	sourcePath, err := normalizeAnsibleSourcePath(data.TaskPath, b.repositoryDirectory)
	if err != nil {
		return ansibleTaskIndex{}, err
	}

	location := ansibleTaskIndex{play: playIndex, task: len(b.report.Plays[playIndex].Tasks)}
	b.report.Plays[playIndex].Tasks = append(b.report.Plays[playIndex].Tasks, AnsibleTask{
		Order:      order,
		ID:         data.TaskUUID,
		Name:       name,
		Action:     data.TaskAction,
		SourcePath: sourcePath,
		Results:    []AnsibleTaskResult{},
	})
	b.taskIndexes[data.TaskUUID] = location
	b.tasks++

	return location, nil
}

func mergeAnsibleTaskPresentation(task *AnsibleTask, data ansibleEventDataSource, repositoryDirectory string) error {
	name := firstNonempty(data.Task, data.Name)
	if name != "" {
		if err := validateAnsibleRequiredText("task name", name, maximumAnsibleNameLength); err != nil {
			return err
		}
		if task.Name != name {
			return fmt.Errorf("task %q has inconsistent name", task.ID)
		}
	}
	if data.TaskAction != "" {
		if err := validateAnsibleRequiredText("task action", data.TaskAction, maximumAnsibleActionLength); err != nil {
			return err
		}
		if task.Action != data.TaskAction {
			return fmt.Errorf("task %q has inconsistent action", task.ID)
		}
	}
	if data.TaskPath != "" {
		sourcePath, err := normalizeAnsibleSourcePath(data.TaskPath, repositoryDirectory)
		if err != nil {
			return err
		}
		if task.SourcePath != sourcePath {
			return fmt.Errorf("task %q has inconsistent source path", task.ID)
		}
	}

	return nil
}

func redactAnsibleTask(task *AnsibleTask) {
	task.Name = ansibleRedactedName
	task.Action = ansibleRedactedName
	task.SourcePath = ""
	task.Redacted = true
}

func (b *ansibleReportBuilder) finish() error {
	var hostTotals []AnsibleHostTotals
	if b.stats != nil {
		var err error
		hostTotals, err = projectAnsibleStats(*b.stats)
		if err != nil {
			return err
		}
	} else {
		hostTotals = calculatePartialAnsibleHostTotals(b.report.Plays)
	}

	b.report.Summary = AnsiblePlaybookSummary{
		Complete:   b.stats != nil,
		Hosts:      len(hostTotals),
		Plays:      len(b.report.Plays),
		Tasks:      b.tasks,
		Results:    b.results,
		HostTotals: hostTotals,
	}

	return nil
}

func projectAnsibleStats(stats ansibleStatsSource) ([]AnsibleHostTotals, error) {
	if len(stats.Processed) > MaximumAnsiblePlaybookHosts {
		return nil, fmt.Errorf("report must contain at most %d hosts", MaximumAnsiblePlaybookHosts)
	}
	for _, required := range []struct {
		name   string
		values map[string]int
	}{
		{name: "changed", values: stats.Changed},
		{name: "dark", values: stats.Dark},
		{name: "failures", values: stats.Failures},
		{name: "ignored", values: stats.Ignored},
		{name: "ok", values: stats.OK},
		{name: "processed", values: stats.Processed},
		{name: "rescued", values: stats.Rescued},
		{name: "skipped", values: stats.Skipped},
	} {
		if required.values == nil {
			return nil, fmt.Errorf("playbook_on_stats event_data.%s is required", required.name)
		}
		for host, count := range required.values {
			if count < 0 || count > ansiblesource.MaximumSourceEvents {
				return nil, fmt.Errorf(
					"playbook_on_stats event_data.%s counts are outside report bounds", required.name,
				)
			}
			if _, found := stats.Processed[host]; !found {
				return nil, fmt.Errorf(
					"playbook_on_stats event_data.%s host %q is missing from processed", required.name, host,
				)
			}
		}
	}

	result := make([]AnsibleHostTotals, 0, len(stats.Processed))
	for host := range stats.Processed {
		totals := AnsibleHostTotals{
			Host:        host,
			OK:          stats.OK[host],
			Changed:     stats.Changed[host],
			Failed:      stats.Failures[host],
			Unreachable: stats.Dark[host],
			Skipped:     stats.Skipped[host],
			Rescued:     stats.Rescued[host],
			Ignored:     stats.Ignored[host],
		}
		if err := totals.validate("playbook_on_stats"); err != nil {
			return nil, err
		}
		result = append(result, totals)
	}
	slices.SortFunc(result, func(left, right AnsibleHostTotals) int {
		return strings.Compare(left.Host, right.Host)
	})

	return result, nil
}

func ansibleEventOutcome(event string, changed bool) (AnsibleOutcome, error) {
	switch event {
	case "runner_on_ok", "runner_item_on_ok", "runner_on_async_ok":
		if changed {
			return AnsibleOutcomeChanged, nil
		}

		return AnsibleOutcomeOK, nil
	case "runner_on_failed", "runner_item_on_failed", "runner_on_async_failed":
		return AnsibleOutcomeFailed, nil
	case "runner_on_unreachable":
		return AnsibleOutcomeUnreachable, nil
	case "runner_on_skipped", "runner_item_on_skipped":
		return AnsibleOutcomeSkipped, nil
	default:
		return "", fmt.Errorf("ansible event %q does not have a projected outcome", event)
	}
}

func firstNonempty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}

	return ""
}

func normalizeAnsibleSourcePath(value, repositoryDirectory string) (string, error) {
	if value == "" {
		return "", nil
	}
	if err := validateAnsibleRequiredText("task source path", value, maximumAnsibleRawPathLength); err != nil {
		return "", err
	}

	value = stripAnsiblePathPosition(value)
	if path.IsAbs(value) {
		relative, inside := strings.CutPrefix(value, strings.TrimSuffix(repositoryDirectory, "/")+"/")
		if !inside {
			return "", nil
		}
		value = relative
	}

	cleaned := path.Clean(value)
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return "", errors.New("task source path must be repository-relative")
	}
	if err := validateAnsibleRequiredText("task source path", cleaned, maximumAnsibleSourcePathLength); err != nil {
		return "", err
	}

	return cleaned, nil
}

func stripAnsiblePathPosition(value string) string {
	separator := strings.LastIndexByte(value, ':')
	if separator < 0 || separator == len(value)-1 {
		return value
	}
	if _, err := strconv.Atoi(value[separator+1:]); err != nil {
		return value
	}

	return value[:separator]
}
