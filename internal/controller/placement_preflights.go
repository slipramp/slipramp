package controller

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/slipramp/slipramp/internal/api"
)

const (
	preflightIDLabel            = "slipramp.com/preflight-id"
	preflightContainerName      = "probe"
	preflightCleanupTimeout     = 30 * time.Second
	maximumPreflightReason      = 200
	maximumPreflightMessage     = 4096
	maximumDiagnosticMessage    = 1024
	maximumPreflightDiagnostics = 20
)

type placementPreflightHandle struct {
	id        uuid.UUID
	namespace string
	name      string
	uid       string
}

type placementPreflightObservation struct {
	finished    bool
	succeeded   bool
	reason      string
	message     string
	diagnostics []api.PlacementPreflightDiagnostic
}

type placementPreflightJobs interface {
	Ensure(context.Context, api.PlacementPreflightCommand) (placementPreflightHandle, error)
	Observe(context.Context, placementPreflightHandle) (placementPreflightObservation, error)
	Delete(context.Context, placementPreflightHandle) error
}

type kubernetesPlacementPreflightJobs struct {
	config Config
	jobs   batchv1client.BatchV1Interface
	pods   corev1client.PodsGetter
	events corev1client.EventsGetter
}

func newKubernetesPlacementPreflightJobs(
	config Config,
	jobs batchv1client.BatchV1Interface,
	core corev1client.CoreV1Interface,
) *kubernetesPlacementPreflightJobs {
	return &kubernetesPlacementPreflightJobs{config: config, jobs: jobs, pods: core, events: core}
}

func (j *kubernetesPlacementPreflightJobs) Ensure(
	ctx context.Context,
	command api.PlacementPreflightCommand,
) (placementPreflightHandle, error) {
	if command.ID == uuid.Nil || command.PlacementName == "" || command.ConfigurationChecksum == "" {
		return placementPreflightHandle{}, errors.New("placement preflight identity is incomplete")
	}
	if command.TimeoutSeconds < 10 || command.TimeoutSeconds > 600 {
		return placementPreflightHandle{}, errors.New("placement preflight timeout is invalid")
	}
	if string(command.Cluster) != j.config.ClusterName {
		return placementPreflightHandle{}, fmt.Errorf(
			"preflight cluster %q does not match Controller cluster %q",
			command.Cluster,
			j.config.ClusterName,
		)
	}
	if !j.config.OwnsExecutionNamespace(string(command.JobNamespace)) {
		return placementPreflightHandle{}, fmt.Errorf(
			"preflight namespace %q is not owned by this Controller",
			command.JobNamespace,
		)
	}
	job, err := renderPlacementPreflightJob(j.config, command)
	if err != nil {
		return placementPreflightHandle{}, err
	}
	handle := placementPreflightHandle{
		id: command.ID, namespace: job.Namespace, name: job.Name,
	}

	created, err := j.jobs.Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err == nil {
		if err := validatePlacementPreflightJob(j.config.ClusterName, handle, created); err != nil {
			return placementPreflightHandle{}, err
		}
		handle.uid = string(created.UID)
		return handle, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return placementPreflightHandle{}, fmt.Errorf("create Placement preflight Job: %w", err)
	}

	existing, err := j.jobs.Jobs(job.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil {
		return placementPreflightHandle{}, fmt.Errorf("get existing Placement preflight Job: %w", err)
	}
	if err := validatePlacementPreflightJob(j.config.ClusterName, handle, existing); err != nil {
		return placementPreflightHandle{}, err
	}
	handle.uid = string(existing.UID)
	return handle, nil
}

func renderPlacementPreflightJob(
	config Config,
	command api.PlacementPreflightCommand,
) (*batchv1.Job, error) {
	template, err := podTemplateFromAPI(command.PodTemplate)
	if err != nil {
		return nil, err
	}
	probeLabels := map[string]string{
		managedByLabel:   managedByLabelValue,
		preflightIDLabel: command.ID.String(),
		clusterLabel:     config.ClusterName,
	}
	if template.Labels == nil {
		template.Labels = map[string]string{}
	}
	maps.Copy(template.Labels, probeLabels)
	resources, err := parseResourceRequirements(resourceRequirementValues(command.Resources))
	if err != nil {
		return nil, err
	}

	template.Spec.Containers = []corev1.Container{{
		Name:      preflightContainerName,
		Image:     config.CheckoutImage,
		Command:   []string{"/bin/sh", "-c", "exit 0"},
		Resources: resources,
	}}
	template.Spec.InitContainers = nil
	template.Spec.RestartPolicy = corev1.RestartPolicyNever
	template.Spec.ServiceAccountName = valueOrEmpty(command.ServiceAccountName)
	template.Spec.NodeSelector = cloneOptionalMap(command.NodeSelector)

	backoffLimit := int32(0)
	deadline := int64(command.TimeoutSeconds)
	name := placementPreflightJobName(command.ID)
	metadata := metav1.ObjectMeta{
		Name: name, Namespace: string(command.JobNamespace), Labels: maps.Clone(probeLabels),
	}
	return &batchv1.Job{
		ObjectMeta: metadata,
		Spec: batchv1.JobSpec{
			BackoffLimit:          &backoffLimit,
			ActiveDeadlineSeconds: &deadline,
			Template:              template,
		},
	}, nil
}

func placementPreflightJobName(id uuid.UUID) string {
	return "slipramp-preflight-" + id.String()
}

func validatePlacementPreflightJob(
	clusterName string,
	handle placementPreflightHandle,
	job *batchv1.Job,
) error {
	if job.Namespace != handle.namespace || job.Name != handle.name ||
		job.Labels[managedByLabel] != managedByLabelValue ||
		job.Labels[preflightIDLabel] != handle.id.String() ||
		job.Labels[clusterLabel] != clusterName || job.UID == "" ||
		(handle.uid != "" && string(job.UID) != handle.uid) {
		return errors.New("placement preflight Job identity does not match the command")
	}
	return nil
}

func (j *kubernetesPlacementPreflightJobs) Observe(
	ctx context.Context,
	handle placementPreflightHandle,
) (placementPreflightObservation, error) {
	job, err := j.jobs.Jobs(handle.namespace).Get(ctx, handle.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return placementPreflightObservation{
			finished: true, reason: "JobDisappeared", message: "The preflight Job disappeared before completion.",
		}, nil
	}
	if err != nil {
		return placementPreflightObservation{}, fmt.Errorf("get Placement preflight Job: %w", err)
	}
	if err := validatePlacementPreflightJob(j.config.ClusterName, handle, job); err != nil {
		return placementPreflightObservation{}, err
	}

	diagnostics := j.placementPreflightDiagnostics(ctx, handle, job)
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return placementPreflightObservation{
				finished: true, succeeded: true, reason: "ProbeSucceeded",
				message:     "The Placement successfully launched and completed the probe Job.",
				diagnostics: diagnostics,
			}, nil
		case batchv1.JobFailed:
			return placementPreflightObservation{
				finished:    true,
				reason:      boundedPreflightText(condition.Reason, maximumPreflightReason, "ProbeFailed"),
				message:     boundedPreflightText(condition.Message, maximumPreflightMessage, "The preflight Job failed."),
				diagnostics: diagnostics,
			}, nil
		}
	}

	return placementPreflightObservation{diagnostics: diagnostics}, nil
}

func (j *kubernetesPlacementPreflightJobs) placementPreflightDiagnostics(
	ctx context.Context,
	handle placementPreflightHandle,
	job *batchv1.Job,
) []api.PlacementPreflightDiagnostic {
	uids := []string{string(job.UID)}
	podSelector := labels.Set{
		batchv1.JobNameLabel:       handle.name,
		batchv1.ControllerUidLabel: handle.uid,
		managedByLabel:             managedByLabelValue,
		preflightIDLabel:           handle.id.String(),
		clusterLabel:               j.config.ClusterName,
	}.AsSelector()
	pods, err := j.pods.Pods(handle.namespace).List(ctx, metav1.ListOptions{
		LabelSelector: podSelector.String(),
	})
	if err == nil {
		for index := range pods.Items {
			pod := &pods.Items[index]
			owner := metav1.GetControllerOf(pod)
			if pod.Namespace == handle.namespace && podSelector.Matches(labels.Set(pod.Labels)) &&
				owner != nil && owner.APIVersion == "batch/v1" && owner.Kind == "Job" &&
				owner.Name == handle.name && string(owner.UID) == handle.uid {
				uids = append(uids, string(pod.UID))
			}
		}
	}

	result := make([]api.PlacementPreflightDiagnostic, 0)
	for _, uid := range uids {
		events, err := j.events.Events(handle.namespace).List(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("involvedObject.uid", uid).String(),
		})
		if err != nil {
			continue
		}
		for _, event := range events.Items {
			if event.Type != corev1.EventTypeWarning || len(result) >= maximumPreflightDiagnostics {
				continue
			}
			count := event.Count
			count = max(count, 1)
			diagnostic := api.PlacementPreflightDiagnostic{
				Reason:  boundedPreflightText(event.Reason, maximumPreflightReason, "KubernetesWarning"),
				Message: boundedPreflightText(event.Message, maximumDiagnosticMessage, "Kubernetes reported a warning."),
				Count:   count,
			}
			if !event.FirstTimestamp.IsZero() {
				value := event.FirstTimestamp.UTC()
				diagnostic.FirstObservedAt = &value
			}
			if !event.LastTimestamp.IsZero() {
				value := event.LastTimestamp.UTC()
				diagnostic.LastObservedAt = &value
			}
			result = append(result, diagnostic)
		}
	}
	return result
}

func boundedPreflightText(value string, limit int, fallback string) string {
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) && !unicode.IsSpace(character) {
			return -1
		}
		return character
	}, strings.TrimSpace(value))
	if value == "" {
		value = fallback
	}
	characters := []rune(value)
	if len(characters) > limit {
		value = string(characters[:limit])
	}
	return value
}

func (j *kubernetesPlacementPreflightJobs) Delete(
	ctx context.Context,
	handle placementPreflightHandle,
) error {
	uid := types.UID(handle.uid)
	propagation := metav1.DeletePropagationForeground
	err := j.jobs.Jobs(handle.namespace).Delete(ctx, handle.name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
		Preconditions:     &metav1.Preconditions{UID: &uid},
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete Placement preflight Job: %w", err)
	}

	for {
		_, err := j.jobs.Jobs(handle.namespace).Get(ctx, handle.name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("confirm Placement preflight Job deletion: %w", err)
		}
		if !waitForRetry(ctx, jobObservationPeriod) {
			return ctx.Err()
		}
	}
}

func (c *Controller) handlePlacementPreflight(ctx context.Context, command api.PlacementPreflightCommand) {
	c.preflightMutex.Lock()
	if _, active := c.activePreflights[command.ID]; active {
		c.preflightMutex.Unlock()
		return
	}
	c.activePreflights[command.ID] = struct{}{}
	c.preflightMutex.Unlock()

	c.workers.Go(func() {
		defer func() {
			c.preflightMutex.Lock()
			delete(c.activePreflights, command.ID)
			c.preflightMutex.Unlock()
		}()
		c.executePlacementPreflight(ctx, command)
	})
}

func (c *Controller) executePlacementPreflight(ctx context.Context, command api.PlacementPreflightCommand) {
	if c.preflightSlots != nil {
		select {
		case c.preflightSlots <- struct{}{}:
			defer func() { <-c.preflightSlots }()
		case <-ctx.Done():
			return
		}
	}

	handle, err := c.preflightJobs.Ensure(ctx, command)
	if err != nil {
		reason := "MaterializationFailed"
		message := boundedPreflightText(err.Error(), maximumPreflightMessage, "The preflight Job could not be created.")
		c.reportPlacementPreflightUntilAccepted(ctx, command.ID, api.PlacementPreflightReport{
			State:        api.PlacementPreflightReportStateFailed,
			CleanupState: api.PlacementPreflightReportCleanupStateNotRequired,
			Reason:       &reason,
			Message:      &message,
		})
		return
	}

	jobUID := handle.uid
	if !c.reportPlacementPreflightUntilAccepted(ctx, command.ID, api.PlacementPreflightReport{
		State:        api.PlacementPreflightReportStateRunning,
		CleanupState: api.PlacementPreflightReportCleanupStatePending,
		JobUID:       &jobUID,
	}) {
		cleanupContext, cancelCleanup := context.WithTimeout(context.WithoutCancel(ctx), preflightCleanupTimeout)
		_ = c.preflightJobs.Delete(cleanupContext, handle)
		cancelCleanup()
		return
	}

	var terminal placementPreflightObservation
	for {
		observation, err := c.preflightJobs.Observe(ctx, handle)
		if err == nil && observation.finished {
			terminal = observation
			break
		}
		if err != nil {
			c.logger.Warn("observe Placement preflight", "preflight_id", command.ID, "error", err)
		}
		if !waitForRetry(ctx, c.observeEvery) {
			return
		}
	}

	cleanupState := api.PlacementPreflightReportCleanupStateSucceeded
	cleanupContext, cancelCleanup := context.WithTimeout(ctx, preflightCleanupTimeout)
	cleanupErr := c.preflightJobs.Delete(cleanupContext, handle)
	cancelCleanup()
	if cleanupErr != nil {
		cleanupState = api.PlacementPreflightReportCleanupStateFailed
		terminal.succeeded = false
		terminal.reason = "CleanupFailed"
		terminal.message = boundedPreflightText(cleanupErr.Error(), maximumPreflightMessage, "The probe Job could not be cleaned up.")
	}

	state := api.PlacementPreflightReportStateFailed
	if terminal.succeeded {
		state = api.PlacementPreflightReportStateSucceeded
	}
	diagnostics := terminal.diagnostics
	reason := boundedPreflightText(terminal.reason, maximumPreflightReason, "ProbeFailed")
	message := boundedPreflightText(terminal.message, maximumPreflightMessage, "The Placement preflight failed.")
	c.reportPlacementPreflightUntilAccepted(ctx, command.ID, api.PlacementPreflightReport{
		State:        state,
		CleanupState: cleanupState,
		JobUID:       &jobUID,
		Reason:       &reason,
		Message:      &message,
		Diagnostics:  &diagnostics,
	})
}

func (c *Controller) reportPlacementPreflightUntilAccepted(
	ctx context.Context,
	preflightID uuid.UUID,
	report api.PlacementPreflightReport,
) bool {
	for {
		if err := c.preflightReports.ReportPlacementPreflight(ctx, preflightID, report); err == nil {
			return true
		} else if permanentServerError(err) {
			if errors.Is(err, errControllerCredentialRejected) && c.stop != nil {
				c.stop(err)
			}
			c.logger.Warn("stop reporting Placement preflight", "preflight_id", preflightID, "error", err)
			return false
		} else if ctx.Err() == nil {
			c.logger.Warn("report Placement preflight", "preflight_id", preflightID, "error", err)
		}
		if !waitForRetry(ctx, c.retryDelay) {
			return false
		}
	}
}
