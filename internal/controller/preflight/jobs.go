package preflight

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
	"github.com/slipramp/slipramp/internal/controller/jobpolicy"
)

const (
	preflightIDLabel            = "slipramp.com/preflight-id"
	preflightContainerName      = "probe"
	deletionObservationPeriod   = 2 * time.Second
	maximumPreflightReason      = 200
	maximumPreflightMessage     = 4096
	maximumDiagnosticMessage    = 1024
	maximumPreflightDiagnostics = 20
)

type Handle struct {
	ID        uuid.UUID
	Namespace string
	Name      string
	UID       string
}

type Observation struct {
	Finished    bool
	Succeeded   bool
	Reason      string
	Message     string
	Diagnostics []api.PlacementPreflightDiagnostic
}

type Manager struct {
	config Config
	jobs   batchv1client.BatchV1Interface
	pods   corev1client.PodsGetter
	events corev1client.EventsGetter
}

func New(
	config Config,
	jobs batchv1client.BatchV1Interface,
	core corev1client.CoreV1Interface,
) *Manager {
	return &Manager{config: config, jobs: jobs, pods: core, events: core}
}

func (j *Manager) Ensure(
	ctx context.Context,
	command api.PlacementPreflightCommand,
) (Handle, error) {
	if command.ID == uuid.Nil || command.PlacementName == "" || command.ConfigurationChecksum == "" {
		return Handle{}, errors.New("placement preflight identity is incomplete")
	}
	if command.TimeoutSeconds < 10 || command.TimeoutSeconds > 600 {
		return Handle{}, errors.New("placement preflight timeout is invalid")
	}
	if string(command.Cluster) != j.config.ClusterName {
		return Handle{}, fmt.Errorf(
			"preflight cluster %q does not match Controller cluster %q",
			command.Cluster,
			j.config.ClusterName,
		)
	}
	if !j.config.ownsExecutionNamespace(string(command.JobNamespace)) {
		return Handle{}, fmt.Errorf(
			"preflight namespace %q is not owned by this Controller",
			command.JobNamespace,
		)
	}
	job, err := renderPlacementPreflightJob(j.config, command)
	if err != nil {
		return Handle{}, err
	}
	handle := Handle{
		ID: command.ID, Namespace: job.Namespace, Name: job.Name,
	}

	created, err := j.jobs.Jobs(job.Namespace).Create(ctx, job, metav1.CreateOptions{})
	if err == nil {
		if err := validatePlacementPreflightJob(j.config.ClusterName, handle, created); err != nil {
			return Handle{}, err
		}
		handle.UID = string(created.UID)
		return handle, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return Handle{}, fmt.Errorf("create Placement preflight Job: %w", err)
	}

	existing, err := j.jobs.Jobs(job.Namespace).Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil {
		return Handle{}, fmt.Errorf("get existing Placement preflight Job: %w", err)
	}
	if err := validatePlacementPreflightJob(j.config.ClusterName, handle, existing); err != nil {
		return Handle{}, err
	}
	handle.UID = string(existing.UID)
	return handle, nil
}

func renderPlacementPreflightJob(
	config Config,
	command api.PlacementPreflightCommand,
) (*batchv1.Job, error) {
	template, err := jobpolicy.PodTemplate(command.PodTemplate)
	if err != nil {
		return nil, err
	}
	probeLabels := map[string]string{
		jobpolicy.ManagedByLabel: jobpolicy.ManagedByLabelValue,
		preflightIDLabel:         command.ID.String(),
		jobpolicy.ClusterLabel:   config.ClusterName,
	}
	if template.Labels == nil {
		template.Labels = map[string]string{}
	}
	maps.Copy(template.Labels, probeLabels)
	resources, err := jobpolicy.Resources(command.Resources, nil)
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
	if command.ServiceAccountName != nil {
		template.Spec.ServiceAccountName = *command.ServiceAccountName
	}
	if command.NodeSelector != nil {
		template.Spec.NodeSelector = maps.Clone(*command.NodeSelector)
	}

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
	handle Handle,
	job *batchv1.Job,
) error {
	if job.Namespace != handle.Namespace || job.Name != handle.Name ||
		job.Labels[jobpolicy.ManagedByLabel] != jobpolicy.ManagedByLabelValue ||
		job.Labels[preflightIDLabel] != handle.ID.String() ||
		job.Labels[jobpolicy.ClusterLabel] != clusterName || job.UID == "" ||
		(handle.UID != "" && string(job.UID) != handle.UID) {
		return errors.New("placement preflight Job identity does not match the command")
	}
	return nil
}

func (j *Manager) Observe(
	ctx context.Context,
	handle Handle,
) (Observation, error) {
	job, err := j.jobs.Jobs(handle.Namespace).Get(ctx, handle.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return Observation{
			Finished: true, Reason: "JobDisappeared", Message: "The preflight Job disappeared before completion.",
		}, nil
	}
	if err != nil {
		return Observation{}, fmt.Errorf("get Placement preflight Job: %w", err)
	}
	if err := validatePlacementPreflightJob(j.config.ClusterName, handle, job); err != nil {
		return Observation{}, err
	}

	diagnostics := j.placementPreflightDiagnostics(ctx, handle, job)
	for _, condition := range job.Status.Conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}
		switch condition.Type {
		case batchv1.JobComplete:
			return Observation{
				Finished: true, Succeeded: true, Reason: "ProbeSucceeded",
				Message:     "The Placement successfully launched and completed the probe Job.",
				Diagnostics: diagnostics,
			}, nil
		case batchv1.JobFailed:
			return Observation{
				Finished:    true,
				Reason:      boundedPreflightText(condition.Reason, maximumPreflightReason, "ProbeFailed"),
				Message:     boundedPreflightText(condition.Message, maximumPreflightMessage, "The preflight Job failed."),
				Diagnostics: diagnostics,
			}, nil
		}
	}

	return Observation{Diagnostics: diagnostics}, nil
}

func (j *Manager) placementPreflightDiagnostics(
	ctx context.Context,
	handle Handle,
	job *batchv1.Job,
) []api.PlacementPreflightDiagnostic {
	uids := []string{string(job.UID)}
	podSelector := labels.Set{
		batchv1.JobNameLabel:       handle.Name,
		batchv1.ControllerUidLabel: handle.UID,
		jobpolicy.ManagedByLabel:   jobpolicy.ManagedByLabelValue,
		preflightIDLabel:           handle.ID.String(),
		jobpolicy.ClusterLabel:     j.config.ClusterName,
	}.AsSelector()
	pods, err := j.pods.Pods(handle.Namespace).List(ctx, metav1.ListOptions{
		LabelSelector: podSelector.String(),
	})
	if err == nil {
		for index := range pods.Items {
			pod := &pods.Items[index]
			owner := metav1.GetControllerOf(pod)
			if pod.Namespace == handle.Namespace && podSelector.Matches(labels.Set(pod.Labels)) &&
				owner != nil && owner.APIVersion == "batch/v1" && owner.Kind == "Job" &&
				owner.Name == handle.Name && string(owner.UID) == handle.UID {
				uids = append(uids, string(pod.UID))
			}
		}
	}

	result := make([]api.PlacementPreflightDiagnostic, 0)
	for _, uid := range uids {
		events, err := j.events.Events(handle.Namespace).List(ctx, metav1.ListOptions{
			FieldSelector: fields.OneTermEqualSelector("involvedObject.UID", uid).String(),
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

func BoundReason(value, fallback string) string {
	return boundedPreflightText(value, maximumPreflightReason, fallback)
}

func BoundMessage(value, fallback string) string {
	return boundedPreflightText(value, maximumPreflightMessage, fallback)
}

func (j *Manager) Delete(
	ctx context.Context,
	handle Handle,
) error {
	uid := types.UID(handle.UID)
	propagation := metav1.DeletePropagationForeground
	err := j.jobs.Jobs(handle.Namespace).Delete(ctx, handle.Name, metav1.DeleteOptions{
		PropagationPolicy: &propagation,
		Preconditions:     &metav1.Preconditions{UID: &uid},
	})
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete Placement preflight Job: %w", err)
	}

	for {
		_, err := j.jobs.Jobs(handle.Namespace).Get(ctx, handle.Name, metav1.GetOptions{})
		if apierrors.IsNotFound(err) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("confirm Placement preflight Job deletion: %w", err)
		}
		timer := time.NewTimer(deletionObservationPeriod)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
