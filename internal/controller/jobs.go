package controller

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	batchv1client "k8s.io/client-go/kubernetes/typed/batch/v1"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"

	"github.com/slipramp/slipramp/internal/api"
)

type runJobs interface {
	Ensure(ctx context.Context, assignment api.RunAssignment) (ensuredRunJob, error)
	RequestCancellation(
		ctx context.Context,
		command api.CancelRunCommand,
	) (cancellationResult, error)
	Recover(ctx context.Context) ([]jobHandle, error)
	Observe(ctx context.Context, handle jobHandle) (runObservation, error)
	OpenLog(ctx context.Context, handle jobHandle, follow bool) (io.ReadCloser, error)
	Delete(ctx context.Context, handle jobHandle) error
}

var errLogNotReady = errors.New("execution log is not ready")

type cancellationResult struct {
	handle      jobHandle
	jobRetained bool
}

// RequestCancellation marks the execution Pod so the Harness can terminate
// the action and let the Job reach an observable terminal state. If the Job
// has not created a Pod yet, deleting it is safe because no action or log has
// started. A missing Job is also success, which keeps delivery idempotent.
func (r *kubernetesRunJobs) RequestCancellation(
	ctx context.Context,
	command api.CancelRunCommand,
) (cancellationResult, error) {
	if !r.config.OwnsExecutionNamespace(string(command.JobNamespace)) {
		return cancellationResult{}, fmt.Errorf(
			"execution namespace %q is not owned by this Controller",
			command.JobNamespace,
		)
	}

	jobs := r.jobs.Jobs(string(command.JobNamespace))
	name := runJobName(command.ID)
	handle := jobHandle{
		runID:     command.ID,
		namespace: string(command.JobNamespace),
		name:      name,
	}
	if command.JobUID != nil {
		handle.uid = *command.JobUID
	}

	job, err := jobs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return cancellationResult{handle: handle}, nil
	}
	if err != nil {
		return cancellationResult{}, fmt.Errorf(
			"get execution Job to cancel: %w",
			err,
		)
	}

	if job.Labels[managedByLabel] != managedByLabelValue ||
		job.Labels[runIDLabel] != command.ID.String() ||
		job.Labels[clusterLabel] != r.config.ClusterName {
		return cancellationResult{}, fmt.Errorf(
			"execution Job %q does not have the expected Run identity",
			name,
		)
	}

	jobUID := string(job.UID)
	if command.JobUID != nil && jobUID != *command.JobUID {
		return cancellationResult{}, fmt.Errorf(
			"execution Job %q UID %q does not match recorded UID %q",
			name,
			jobUID,
			*command.JobUID,
		)
	}

	if jobUID != "" {
		handle.uid = jobUID
	}

	pods, err := r.pods.Pods(handle.namespace).List(
		ctx,
		metav1.ListOptions{
			LabelSelector: labels.Set{
				batchv1.JobNameLabel: handle.name,
				managedByLabel:       managedByLabelValue,
				runIDLabel:           command.ID.String(),
				clusterLabel:         r.config.ClusterName,
			}.String(),
		},
	)
	if err != nil {
		return cancellationResult{}, fmt.Errorf(
			"list execution Pods to cancel Run: %w",
			err,
		)
	}
	if len(pods.Items) == 0 {
		propagation := metav1.DeletePropagationForeground
		options := metav1.DeleteOptions{PropagationPolicy: &propagation}
		if job.UID != "" {
			options.Preconditions = &metav1.Preconditions{UID: &job.UID}
		}
		if err := jobs.Delete(ctx, name, options); err != nil &&
			!apierrors.IsNotFound(err) {
			return cancellationResult{}, fmt.Errorf(
				"delete unstarted execution Job: %w",
				err,
			)
		}

		return cancellationResult{handle: handle}, nil
	}
	if terminalJobObservation(job.Status.Conditions, nil) != nil ||
		actionAlreadyTerminated(pods.Items) {
		return cancellationResult{handle: handle, jobRetained: true}, nil
	}

	for index := range pods.Items {
		pod := pods.Items[index].DeepCopy()
		if pod.Annotations[cancellationAnnotation] == "true" {
			continue
		}
		if pod.Annotations == nil {
			pod.Annotations = make(map[string]string)
		}
		pod.Annotations[cancellationAnnotation] = "true"
		if _, err := r.pods.Pods(handle.namespace).Update(
			ctx,
			pod,
			metav1.UpdateOptions{},
		); err != nil && !apierrors.IsNotFound(err) {
			return cancellationResult{}, fmt.Errorf(
				"mark execution Pod for cancellation: %w",
				err,
			)
		}
	}

	return cancellationResult{handle: handle, jobRetained: true}, nil
}

func actionAlreadyTerminated(pods []corev1.Pod) bool {
	for podIndex := range pods {
		for statusIndex := range pods[podIndex].Status.ContainerStatuses {
			status := &pods[podIndex].Status.ContainerStatuses[statusIndex]
			if status.Name == actionContainerName && status.State.Terminated != nil {
				return true
			}
		}
	}

	return false
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
	if !r.config.OwnsExecutionNamespace(string(assignment.Placement.JobNamespace)) {
		return ensuredRunJob{}, permanentMaterialization(fmt.Errorf(
			"execution namespace %q is not owned by this Controller",
			assignment.Placement.JobNamespace,
		))
	}

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
	handles := make([]jobHandle, 0)
	for _, namespace := range r.config.ExecutionNamespaces {
		jobs, err := r.jobs.Jobs(namespace).List(
			ctx,
			metav1.ListOptions{LabelSelector: selector},
		)
		if err != nil {
			return nil, fmt.Errorf(
				"list managed execution Jobs in namespace %q: %w",
				namespace,
				err,
			)
		}

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
		return runObservation{}, fmt.Errorf("list execution Pods: %w", err)
	}

	cancellationRequested := false
	for index := range pods.Items {
		if pods.Items[index].Annotations[cancellationAnnotation] == "true" {
			cancellationRequested = true
			break
		}
	}

	for podIndex := range pods.Items {
		for statusIndex := range pods.Items[podIndex].Status.ContainerStatuses {
			status := &pods.Items[podIndex].Status.ContainerStatuses[statusIndex]
			if status.Name != actionContainerName {
				continue
			}

			if status.State.Running != nil {
				if terminal != nil {
					return canceledIfRequested(
						*terminal,
						cancellationRequested,
					), nil
				}

				return runObservation{
					phase:  api.RunObservationPhaseRunning,
					jobUID: uid,
				}, nil
			}
			if status.State.Terminated != nil {
				terminated := status.State.Terminated
				if terminal == nil {
					return canceledIfRequested(
						terminatedObservation(terminated, uid),
						cancellationRequested,
					), nil
				}

				terminal.exitCode = &terminated.ExitCode
				if terminal.reason == nil {
					terminal.reason = optionalNonemptyString(terminated.Reason)
				}
				if terminal.message == nil {
					terminal.message = optionalNonemptyString(terminated.Message)
				}

				return canceledIfRequested(
					*terminal,
					cancellationRequested,
				), nil
			}
		}
	}
	if terminal != nil {
		return canceledIfRequested(*terminal, cancellationRequested), nil
	}

	return runObservation{
		phase:  api.RunObservationPhasePreparing,
		jobUID: uid,
	}, nil
}

func canceledIfRequested(
	observation runObservation,
	requested bool,
) runObservation {
	if !requested || !observation.terminal() {
		return observation
	}

	outcome := api.RunObservationOutcomeCanceled
	reason := "CancelRequested"
	observation.outcome = &outcome
	observation.reason = &reason

	return observation
}

// Delete removes a completed execution Job only when it still has the UID
// captured when observation began. A missing Job is success so cleanup remains
// idempotent when a deletion response is lost or the Controller restarts.
func (r *kubernetesRunJobs) Delete(
	ctx context.Context,
	handle jobHandle,
) error {
	if handle.uid == "" {
		return errors.New("delete execution Job: Job UID is required")
	}

	uid := types.UID(handle.uid)
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{
		Preconditions:     &metav1.Preconditions{UID: &uid},
		PropagationPolicy: &propagation,
	}
	err := r.jobs.Jobs(handle.namespace).Delete(
		ctx,
		handle.name,
		options,
	)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete completed execution Job: %w", err)
	}

	return nil
}

func (r *kubernetesRunJobs) OpenLog(
	ctx context.Context,
	handle jobHandle,
	follow bool,
) (io.ReadCloser, error) {
	pods, err := r.pods.Pods(handle.namespace).List(
		ctx,
		metav1.ListOptions{
			LabelSelector: labels.Set{batchv1.JobNameLabel: handle.name}.String(),
		},
	)
	if err != nil {
		return nil, fmt.Errorf("list execution Pods for log stream: %w", err)
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

	stream, err := r.pods.Pods(handle.namespace).GetLogs(
		pod.Name,
		&corev1.PodLogOptions{
			Container: actionContainerName,
			Follow:    follow,
		},
	).Stream(ctx)
	if err != nil {
		return nil, fmt.Errorf("open execution Pod log stream: %w", err)
	}

	return stream, nil
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
