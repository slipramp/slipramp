package controller

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/slipramp/slipramp/internal/api"
)

type runJobs interface {
	Ensure(ctx context.Context, assignment api.RunAssignment) (ensuredRunJob, error)
	Cancel(ctx context.Context, command api.CancelRunCommand) (*string, error)
	Recover(ctx context.Context) ([]jobHandle, error)
	Observe(ctx context.Context, handle jobHandle) (runObservation, error)
}

var errLogNotReady = errors.New("execution log is not ready")

type runLogReader interface {
	ReadLog(ctx context.Context, handle jobHandle) ([]byte, error)
}

// Cancel removes the deterministic execution Job. A missing Job is success so
// repeated cancel commands and assignments canceled before
// materialization remain idempotent.
func (r *kubernetesRunJobs) Cancel(
	ctx context.Context,
	command api.CancelRunCommand,
) (*string, error) {
	jobs := r.jobs.Jobs(string(command.JobNamespace))
	name := runJobName(command.ID)
	job, err := jobs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return command.JobUID, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get execution Job to cancel: %w", err)
	}

	if job.Labels[managedByLabel] != managedByLabelValue ||
		job.Labels[runIDLabel] != command.ID.String() ||
		job.Labels[clusterLabel] != r.config.ClusterName {
		return nil, fmt.Errorf(
			"execution Job %q does not have the expected Run identity",
			name,
		)
	}

	jobUID := string(job.UID)
	if command.JobUID != nil && jobUID != *command.JobUID {
		return nil, fmt.Errorf(
			"execution Job %q UID %q does not match recorded UID %q",
			name,
			jobUID,
			*command.JobUID,
		)
	}

	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{PropagationPolicy: &propagation}
	if job.UID != "" {
		options.Preconditions = &metav1.Preconditions{UID: &job.UID}
	}
	if err := jobs.Delete(ctx, name, options); err != nil && !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("delete execution Job to cancel Run: %w", err)
	}

	if jobUID == "" {
		return command.JobUID, nil
	}

	return &jobUID, nil
}

type runObservation struct {
	phase    api.RunObservationPhase
	outcome  *api.RunObservationOutcome
	jobUID   *string
	reason   *string
	message  *string
	exitCode *int32
}

func (o runObservation) terminal() bool {
	return o.phase == api.RunObservationPhaseFinished
}

type jobHandle struct {
	runID     uuid.UUID
	namespace string
	name      string
	uid       string
}

type ensuredRunJob struct {
	handle  jobHandle
	created bool
}

type permanentMaterializationError struct {
	err error
}

func (e *permanentMaterializationError) Error() string { return e.err.Error() }
func (e *permanentMaterializationError) Unwrap() error { return e.err }

type kubernetesRunJobs struct {
	config Config
	jobs   batchv1client.BatchV1Interface
	pods   corev1client.PodsGetter
}

func newKubernetesRunJobs(
	config Config,
	jobs batchv1client.BatchV1Interface,
	pods corev1client.PodsGetter,
) *kubernetesRunJobs {
	return &kubernetesRunJobs{config: config, jobs: jobs, pods: pods}
}

// Ensure creates the deterministic Job for an assignment. A matching existing
// Job is success because the Server may deliver the same assignment more than
// once.
func (r *kubernetesRunJobs) Ensure(
	ctx context.Context,
	assignment api.RunAssignment,
) (ensuredRunJob, error) {
	job, err := renderJob(r.config, assignment)
	if err != nil {
		return ensuredRunJob{}, permanentMaterialization(err)
	}

	jobs := r.jobs.Jobs(job.Namespace)
	created, err := jobs.Create(ctx, job, metav1.CreateOptions{})
	if err == nil {
		return ensuredRunJob{
			handle:  handleFromJob(created, assignment.ID),
			created: true,
		}, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return ensuredRunJob{}, classifyMaterializationError(
			"create execution Job",
			err,
		)
	}

	existing, err := jobs.Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil {
		return ensuredRunJob{}, classifyMaterializationError(
			"get existing execution Job",
			err,
		)
	}

	if existing.Labels[managedByLabel] != managedByLabelValue ||
		existing.Labels[runIDLabel] != assignment.ID.String() ||
		existing.Labels[clusterLabel] != r.config.ClusterName {
		return ensuredRunJob{}, permanentMaterialization(fmt.Errorf(
			"execution Job %q already exists without the expected Run identity",
			job.Name,
		))
	}

	return ensuredRunJob{
		handle: handleFromJob(existing, assignment.ID),
	}, nil
}

func (r *kubernetesRunJobs) Recover(ctx context.Context) ([]jobHandle, error) {
	selector := labels.Set{
		managedByLabel: managedByLabelValue,
		clusterLabel:   r.config.ClusterName,
	}.String()
	jobs, err := r.jobs.Jobs(metav1.NamespaceAll).List(
		ctx,
		metav1.ListOptions{LabelSelector: selector},
	)
	if err != nil {
		return nil, fmt.Errorf("list managed execution Jobs: %w", err)
	}

	handles := make([]jobHandle, 0, len(jobs.Items))
	for index := range jobs.Items {
		job := &jobs.Items[index]
		runID, parseErr := uuid.Parse(job.Labels[runIDLabel])
		// A partial or copied label set does not establish Slipramp ownership.
		// Ignore it rather than letting one malformed Job block all recovery.
		if parseErr != nil || job.Name != runJobName(runID) {
			continue
		}

		handles = append(handles, handleFromJob(job, runID))
	}

	return handles, nil
}

func (r *kubernetesRunJobs) Observe(
	ctx context.Context,
	handle jobHandle,
) (runObservation, error) {
	job, err := r.jobs.Jobs(handle.namespace).Get(
		ctx,
		handle.name,
		metav1.GetOptions{},
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			outcome := api.RunObservationOutcomeFailed
			reason := "JobNotFound"
			message := fmt.Sprintf(
				"execution Job %s/%s no longer exists",
				handle.namespace,
				handle.name,
			)

			return runObservation{
				phase:   api.RunObservationPhaseFinished,
				outcome: &outcome,
				jobUID:  optionalNonemptyString(handle.uid),
				reason:  &reason,
				message: &message,
			}, nil
		}

		return runObservation{}, fmt.Errorf("get execution Job state: %w", err)
	}
	jobUID := string(job.UID)
	if jobUID == "" {
		jobUID = handle.uid
	}
	uid := optionalNonemptyString(jobUID)

	terminal := terminalJobObservation(job.Status.Conditions, uid)

	pods, err := r.pods.Pods(handle.namespace).List(
		ctx,
		metav1.ListOptions{
			LabelSelector: labels.Set{
				batchv1.JobNameLabel: handle.name,
			}.String(),
		},
	)
	if err != nil {
		if terminal != nil {
			return *terminal, nil
		}

		return runObservation{}, fmt.Errorf("list execution Pods: %w", err)
	}

	for podIndex := range pods.Items {
		for statusIndex := range pods.Items[podIndex].Status.ContainerStatuses {
			status := &pods.Items[podIndex].Status.ContainerStatuses[statusIndex]
			if status.Name != actionContainerName {
				continue
			}

			if status.State.Running != nil {
				if terminal != nil {
					return *terminal, nil
				}

				return runObservation{
					phase:  api.RunObservationPhaseRunning,
					jobUID: uid,
				}, nil
			}
			if status.State.Terminated != nil {
				terminated := status.State.Terminated
				if terminal == nil {
					return terminatedObservation(terminated, uid), nil
				}

				terminal.exitCode = &terminated.ExitCode
				if terminal.reason == nil {
					terminal.reason = optionalNonemptyString(terminated.Reason)
				}
				if terminal.message == nil {
					terminal.message = optionalNonemptyString(terminated.Message)
				}

				return *terminal, nil
			}
		}
	}
	if terminal != nil {
		return *terminal, nil
	}

	return runObservation{
		phase:  api.RunObservationPhasePreparing,
		jobUID: uid,
	}, nil
}

func (r *kubernetesRunJobs) ReadLog(
	ctx context.Context,
	handle jobHandle,
) ([]byte, error) {
	pods, err := r.pods.Pods(handle.namespace).List(
		ctx,
		metav1.ListOptions{
			LabelSelector: labels.Set{batchv1.JobNameLabel: handle.name}.String(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list execution Pods for logs: %w", err)
	}
	if len(pods.Items) == 0 {
		return nil, errLogNotReady
	}
	sort.Slice(pods.Items, func(left, right int) bool {
		return pods.Items[left].CreationTimestamp.Before(
			&pods.Items[right].CreationTimestamp,
		)
	})
	pod := pods.Items[0]
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == actionContainerName &&
			status.State.Waiting != nil {
			return nil, errLogNotReady
		}
	}

	contents, err := r.pods.Pods(handle.namespace).GetLogs(
		pod.Name,
		&corev1.PodLogOptions{Container: actionContainerName},
	).DoRaw(ctx)
	if err != nil {
		return nil, fmt.Errorf("read execution Pod log: %w", err)
	}

	return contents, nil
}

func terminalJobObservation(
	conditions []batchv1.JobCondition,
	jobUID *string,
) *runObservation {
	for _, condition := range conditions {
		if condition.Status != corev1.ConditionTrue {
			continue
		}

		outcome := api.RunObservationOutcomeFailed
		var exitCode *int32
		switch condition.Type {
		case batchv1.JobComplete:
			outcome = api.RunObservationOutcomeSucceeded
			exitCode = new(int32(0))
		case batchv1.JobFailed:
			if condition.Reason == "DeadlineExceeded" {
				outcome = api.RunObservationOutcomeTimedOut
			}
		default:
			continue
		}

		return &runObservation{
			phase:    api.RunObservationPhaseFinished,
			outcome:  &outcome,
			jobUID:   jobUID,
			reason:   optionalNonemptyString(condition.Reason),
			message:  optionalNonemptyString(condition.Message),
			exitCode: exitCode,
		}
	}

	return nil
}

func handleFromJob(job *batchv1.Job, runID uuid.UUID) jobHandle {
	return jobHandle{
		runID:     runID,
		namespace: job.Namespace,
		name:      job.Name,
		uid:       string(job.UID),
	}
}

func terminatedObservation(
	terminated *corev1.ContainerStateTerminated,
	jobUID *string,
) runObservation {
	outcome := api.RunObservationOutcomeFailed
	if terminated.ExitCode == 0 {
		outcome = api.RunObservationOutcomeSucceeded
	}

	return runObservation{
		phase:    api.RunObservationPhaseFinished,
		outcome:  &outcome,
		jobUID:   jobUID,
		reason:   optionalNonemptyString(terminated.Reason),
		message:  optionalNonemptyString(terminated.Message),
		exitCode: &terminated.ExitCode,
	}
}

func optionalNonemptyString(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return &value
}

func permanentMaterialization(err error) error {
	return &permanentMaterializationError{err: err}
}

func isPermanentMaterializationError(err error) bool {
	var target *permanentMaterializationError

	return errors.As(err, &target)
}

func classifyMaterializationError(operation string, err error) error {
	wrapped := fmt.Errorf("%s: %w", operation, err)
	if apierrors.IsBadRequest(err) || apierrors.IsForbidden(err) ||
		apierrors.IsInvalid(err) || apierrors.IsUnauthorized(err) {
		return permanentMaterialization(wrapped)
	}

	return wrapped
}
