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
	RequestCancellation(ctx context.Context, command api.CancelRunCommand) (cancellationResult, error)
	Recover(ctx context.Context) ([]jobHandle, error)
	Observe(ctx context.Context, handle jobHandle) (runObservation, error)
	OpenLog(ctx context.Context, handle jobHandle, follow bool) (io.ReadCloser, error)
	Delete(ctx context.Context, handle jobHandle) error
}

var (
	errLogNotReady            = errors.New("execution log is not ready")
	errRunJobIdentityMismatch = errors.New("execution Job identity does not match Run")
)

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
	if err := r.validateJobHandle(handle); err != nil {
		return cancellationResult{}, err
	}

	job, err := jobs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return cancellationResult{handle: handle}, nil
	}
	if err != nil {
		return cancellationResult{}, fmt.Errorf("get execution Job to cancel: %w", err)
	}

	if err := r.validateJobIdentity(handle, job); err != nil {
		return cancellationResult{}, err
	}
	handle.uid = string(job.UID)

	pods, err := r.executionPods(ctx, handle)
	if err != nil {
		return cancellationResult{}, fmt.Errorf("list execution Pods to cancel Run: %w", err)
	}
	if len(pods) == 0 {
		propagation := metav1.DeletePropagationForeground
		options := metav1.DeleteOptions{PropagationPolicy: &propagation}
		options.Preconditions = &metav1.Preconditions{UID: &job.UID}
		if err := jobs.Delete(ctx, name, options); err != nil &&
			!apierrors.IsNotFound(err) {
			return cancellationResult{}, fmt.Errorf("delete unstarted execution Job: %w", err)
		}

		return cancellationResult{handle: handle}, nil
	}
	if terminalJobObservation(job.Status.Conditions, nil) != nil ||
		actionAlreadyTerminated(pods) {
		return cancellationResult{handle: handle, jobRetained: true}, nil
	}

	for index := range pods {
		pod := pods[index].DeepCopy()
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
			return cancellationResult{}, fmt.Errorf("mark execution Pod for cancellation: %w", err)
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
	images   api.ExecutionImages
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
func (r *kubernetesRunJobs) Ensure(ctx context.Context, assignment api.RunAssignment) (ensuredRunJob, error) {
	// Reject invalid assignments before accessing Kubernetes. These failures do
	// not establish that a foreign or replacement Job exists and can finish the Run.
	if !r.config.OwnsExecutionNamespace(string(assignment.Placement.JobNamespace)) {
		return ensuredRunJob{}, permanentMaterialization(fmt.Errorf(
			"execution namespace %q is not owned by this Controller", assignment.Placement.JobNamespace,
		))
	}
	if assignment.ID == uuid.Nil {
		return ensuredRunJob{}, permanentMaterialization(errors.New("Run ID is required"))
	}
	handle := jobHandle{
		runID:     assignment.ID,
		namespace: string(assignment.Placement.JobNamespace),
		name:      runJobName(assignment.ID),
	}
	job, err := renderJob(r.config, assignment)
	if err != nil {
		return ensuredRunJob{}, permanentMaterialization(err)
	}

	jobs := r.jobs.Jobs(job.Namespace)
	created, err := jobs.Create(ctx, job, metav1.CreateOptions{})
	if err == nil {
		if err := r.validateJobIdentity(handle, created); err != nil {
			return ensuredRunJob{}, permanentMaterialization(err)
		}

		return ensuredRunJob{
			handle:  handleFromJob(created, assignment.ID),
			created: true,
		}, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return ensuredRunJob{}, classifyMaterializationError("create execution Job", err)
	}

	existing, err := jobs.Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil {
		return ensuredRunJob{}, classifyMaterializationError("get existing execution Job", err)
	}

	if err := r.validateJobIdentity(handle, existing); err != nil {
		return ensuredRunJob{}, permanentMaterialization(err)
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
		jobs, err := r.jobs.Jobs(namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return nil, fmt.Errorf("list managed execution Jobs in namespace %q: %w", namespace, err)
		}

		for index := range jobs.Items {
			job := &jobs.Items[index]
			runID, parseErr := uuid.Parse(job.Labels[runIDLabel])
			// A partial or copied label set does not establish Slipramp ownership.
			// Ignore it rather than letting one malformed Job block all recovery.
			if parseErr != nil {
				continue
			}
			handle := jobHandle{runID: runID, namespace: namespace, name: runJobName(runID)}
			if err := r.validateJobIdentity(handle, job); err != nil {
				continue
			}

			handles = append(handles, handleFromJob(job, runID))
		}
	}

	return handles, nil
}

func (r *kubernetesRunJobs) Observe(ctx context.Context, handle jobHandle) (runObservation, error) {
	if err := r.validateJobHandle(handle); err != nil {
		return runObservation{}, err
	}

	job, err := r.jobs.Jobs(handle.namespace).Get(ctx, handle.name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			outcome := api.RunObservationOutcomeFailed
			reason := "JobNotFound"
			message := fmt.Sprintf("execution Job %s/%s no longer exists", handle.namespace, handle.name)

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
	if err := r.validateJobIdentity(handle, job); err != nil {
		return runObservation{}, err
	}
	handle.uid = string(job.UID)
	uid := new(handle.uid)

	terminal := terminalJobObservation(job.Status.Conditions, uid)

	pods, err := r.executionPods(ctx, handle)
	if err != nil {
		return runObservation{}, fmt.Errorf("list execution Pods: %w", err)
	}

	cancellationRequested := false
	for index := range pods {
		if pods[index].Annotations[cancellationAnnotation] == "true" {
			cancellationRequested = true
			break
		}
	}
	images, err := observedExecutionImages(pods)
	if err != nil {
		return runObservation{}, err
	}
	if terminal != nil {
		terminal.images = images
	}

	for podIndex := range pods {
		for statusIndex := range pods[podIndex].Status.ContainerStatuses {
			status := &pods[podIndex].Status.ContainerStatuses[statusIndex]
			if status.Name != actionContainerName {
				continue
			}

			if status.State.Running != nil {
				if terminal != nil {
					return canceledIfRequested(*terminal, cancellationRequested), nil
				}

				return runObservation{
					phase:  api.RunObservationPhaseRunning,
					jobUID: uid,
					images: images,
				}, nil
			}
			if status.State.Terminated != nil {
				terminated := status.State.Terminated
				if terminal == nil {
					observation := terminatedObservation(terminated, uid)
					observation.images = images

					return canceledIfRequested(observation, cancellationRequested), nil
				}

				terminal.exitCode = &terminated.ExitCode
				if terminal.reason == nil {
					terminal.reason = optionalNonemptyString(terminated.Reason)
				}
				if terminal.message == nil {
					terminal.message = optionalNonemptyString(terminated.Message)
				}

				return canceledIfRequested(*terminal, cancellationRequested), nil
			}
		}
	}
	if terminal != nil {
		return canceledIfRequested(*terminal, cancellationRequested), nil
	}

	return runObservation{
		phase:  api.RunObservationPhasePreparing,
		jobUID: uid,
		images: images,
	}, nil
}

func observedExecutionImages(pods []corev1.Pod) (api.ExecutionImages, error) {
	images := api.ExecutionImages{}
	for podIndex := range pods {
		for statusIndex := range pods[podIndex].Status.InitContainerStatuses {
			status := &pods[podIndex].Status.InitContainerStatuses[statusIndex]
			var destination **string
			switch status.Name {
			case harnessContainerName:
				destination = &images.Harness
			case checkoutContainerName:
				destination = &images.Checkout
			default:
				continue
			}
			if err := recordObservedImageID(status.Name, status.ImageID, destination); err != nil {
				return api.ExecutionImages{}, err
			}
		}
		for statusIndex := range pods[podIndex].Status.ContainerStatuses {
			status := &pods[podIndex].Status.ContainerStatuses[statusIndex]
			if status.Name != actionContainerName {
				continue
			}
			if err := recordObservedImageID(status.Name, status.ImageID, &images.Action); err != nil {
				return api.ExecutionImages{}, err
			}
		}
	}

	return images, nil
}

func recordObservedImageID(containerName, imageID string, destination **string) error {
	if imageID == "" {
		return nil
	}
	if *destination != nil && **destination != imageID {
		return fmt.Errorf(
			"execution container %q has conflicting image IDs %q and %q",
			containerName,
			**destination,
			imageID,
		)
	}

	*destination = new(imageID)

	return nil
}

func canceledIfRequested(observation runObservation, requested bool) runObservation {
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
func (r *kubernetesRunJobs) Delete(ctx context.Context, handle jobHandle) error {
	if err := r.validateJobHandle(handle); err != nil {
		return err
	}
	if handle.uid == "" {
		return fmt.Errorf("%w: delete execution Job requires a pinned UID", errRunJobIdentityMismatch)
	}
	job, err := r.jobs.Jobs(handle.namespace).Get(ctx, handle.name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get completed execution Job before deletion: %w", err)
	}
	if err := r.validateJobIdentity(handle, job); err != nil {
		return err
	}

	uid := types.UID(handle.uid)
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{
		Preconditions:     &metav1.Preconditions{UID: &uid},
		PropagationPolicy: &propagation,
	}
	err = r.jobs.Jobs(handle.namespace).Delete(ctx, handle.name, options)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete completed execution Job: %w", err)
	}

	return nil
}

func (r *kubernetesRunJobs) OpenLog(ctx context.Context, handle jobHandle, follow bool) (io.ReadCloser, error) {
	if err := r.validateJobHandle(handle); err != nil {
		return nil, err
	}
	job, err := r.jobs.Jobs(handle.namespace).Get(ctx, handle.name, metav1.GetOptions{})
	if err == nil {
		if err := r.validateJobIdentity(handle, job); err != nil {
			return nil, err
		}
		handle.uid = string(job.UID)
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get execution Job for log stream: %w", err)
	}
	// Original Pods can outlive a deleted Job. Its pinned UID still establishes
	// which remaining log source belongs to this Run.
	if handle.uid == "" {
		return nil, errLogNotReady
	}

	pods, err := r.executionPods(ctx, handle)
	if err != nil {
		return nil, fmt.Errorf("list execution Pods for log stream: %w", err)
	}
	if len(pods) == 0 {
		return nil, errLogNotReady
	}
	sort.Slice(pods, func(left, right int) bool {
		return pods[left].CreationTimestamp.Before(&pods[right].CreationTimestamp)
	})
	pod := pods[0]
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

func (r *kubernetesRunJobs) validateJobHandle(handle jobHandle) error {
	if !r.config.OwnsExecutionNamespace(handle.namespace) {
		return fmt.Errorf(
			"%w: execution namespace %q is not owned by this Controller",
			errRunJobIdentityMismatch,
			handle.namespace,
		)
	}
	if handle.runID == uuid.Nil || handle.name != runJobName(handle.runID) {
		return fmt.Errorf("%w: Job name %q does not match Run %s", errRunJobIdentityMismatch, handle.name, handle.runID)
	}

	return nil
}

// An unbound handle may learn an admitted UID once. A pinned handle must never
// adopt a replacement Job, even when its deterministic name and labels match.
func (r *kubernetesRunJobs) validateJobIdentity(handle jobHandle, job *batchv1.Job) error {
	if err := r.validateJobHandle(handle); err != nil {
		return err
	}
	if job.Namespace != handle.namespace || job.Name != handle.name ||
		job.Labels[managedByLabel] != managedByLabelValue ||
		job.Labels[runIDLabel] != handle.runID.String() ||
		job.Labels[clusterLabel] != r.config.ClusterName {
		return fmt.Errorf("%w: Job %s/%s does not have the expected Run identity",
			errRunJobIdentityMismatch, job.Namespace, job.Name)
	}
	if job.UID == "" {
		return fmt.Errorf("%w: Job %s/%s has no admitted UID", errRunJobIdentityMismatch, job.Namespace, job.Name)
	}
	if handle.uid != "" && string(job.UID) != handle.uid {
		return fmt.Errorf("%w: Job %s/%s UID %q does not match pinned UID %q",
			errRunJobIdentityMismatch, job.Namespace, job.Name, job.UID, handle.uid)
	}

	return nil
}

func (r *kubernetesRunJobs) executionPods(ctx context.Context, handle jobHandle) ([]corev1.Pod, error) {
	if handle.uid == "" {
		return nil, fmt.Errorf("%w: select execution Pods requires a pinned Job UID", errRunJobIdentityMismatch)
	}
	selector := labels.Set{
		batchv1.JobNameLabel:       handle.name,
		batchv1.ControllerUidLabel: handle.uid,
		managedByLabel:             managedByLabelValue,
		runIDLabel:                 handle.runID.String(),
		clusterLabel:               r.config.ClusterName,
	}
	podSelector := selector.AsSelector()
	pods, err := r.pods.Pods(handle.namespace).List(ctx, metav1.ListOptions{LabelSelector: podSelector.String()})
	if err != nil {
		return nil, err
	}
	for index := range pods.Items {
		pod := &pods.Items[index]
		owner := metav1.GetControllerOf(pod)
		if pod.Namespace != handle.namespace || !podSelector.Matches(labels.Set(pod.Labels)) ||
			owner == nil || owner.APIVersion != "batch/v1" || owner.Kind != "Job" ||
			owner.Name != handle.name || string(owner.UID) != handle.uid {
			return nil, fmt.Errorf("%w: Pod %s/%s is not controlled by Job %s with UID %q",
				errRunJobIdentityMismatch, pod.Namespace, pod.Name, handle.name, handle.uid)
		}
	}

	return pods.Items, nil
}

func terminalJobObservation(conditions []batchv1.JobCondition, jobUID *string) *runObservation {
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

func terminatedObservation(terminated *corev1.ContainerStateTerminated, jobUID *string) runObservation {
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
