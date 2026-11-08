package runjob

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
	"github.com/slipramp/slipramp/internal/controller/jobpolicy"
	"github.com/slipramp/slipramp/internal/harness"
)

var (
	ErrLogNotReady            = errors.New("execution log is not ready")
	ErrRunJobIdentityMismatch = errors.New("execution Job identity does not match Run")
)

type CancellationResult struct {
	Handle      Handle
	JobRetained bool
}

// RequestCancellation marks the execution Pod so the Harness can terminate
// the action and let the Job reach an observable terminal state. If the Job
// has not created a Pod yet, deleting it is safe because no action or log has
// started. A missing Job is also success, which keeps delivery idempotent.
func (r *Manager) RequestCancellation(
	ctx context.Context,
	command api.CancelRunCommand,
) (CancellationResult, error) {
	jobs := r.jobs.Jobs(string(command.JobNamespace))
	name := runJobName(command.ID)
	handle := Handle{
		RunID:     command.ID,
		Namespace: string(command.JobNamespace),
		Name:      name,
	}
	if command.JobUID != nil {
		handle.UID = *command.JobUID
	}
	if err := r.validateJobHandle(handle); err != nil {
		return CancellationResult{}, err
	}

	job, err := jobs.Get(ctx, name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return CancellationResult{Handle: handle}, nil
	}
	if err != nil {
		return CancellationResult{}, fmt.Errorf("get execution Job to cancel: %w", err)
	}

	if err := r.validateJobIdentity(handle, job); err != nil {
		return CancellationResult{}, err
	}
	handle.UID = string(job.UID)

	pods, err := r.executionPods(ctx, handle)
	if err != nil {
		return CancellationResult{}, fmt.Errorf("list execution Pods to cancel Run: %w", err)
	}
	if len(pods) == 0 {
		propagation := metav1.DeletePropagationForeground
		options := metav1.DeleteOptions{PropagationPolicy: &propagation}
		options.Preconditions = &metav1.Preconditions{UID: &job.UID}
		if err := jobs.Delete(ctx, name, options); err != nil &&
			!apierrors.IsNotFound(err) {
			return CancellationResult{}, fmt.Errorf("delete unstarted execution Job: %w", err)
		}

		return CancellationResult{Handle: handle}, nil
	}
	if terminalJobObservation(job.Status.Conditions, nil, false) != nil ||
		actionAlreadyTerminated(pods) {
		return CancellationResult{Handle: handle, JobRetained: true}, nil
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
		if _, err := r.pods.Pods(handle.Namespace).Update(
			ctx,
			pod,
			metav1.UpdateOptions{},
		); err != nil && !apierrors.IsNotFound(err) {
			return CancellationResult{}, fmt.Errorf("mark execution Pod for cancellation: %w", err)
		}
	}

	return CancellationResult{Handle: handle, JobRetained: true}, nil
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

type Observation struct {
	Phase    api.RunObservationPhase
	Outcome  *api.RunObservationOutcome
	JobUID   *string
	Images   api.ExecutionImages
	Reason   *string
	Message  *string
	ExitCode *int32
}

func (o Observation) Terminal() bool {
	return o.Phase == api.RunObservationPhaseFinished
}

type Handle struct {
	RunID     uuid.UUID
	Namespace string
	Name      string
	UID       string
}

type Ensured struct {
	Handle  Handle
	Created bool
}

type permanentMaterializationError struct {
	err error
}

func (e *permanentMaterializationError) Error() string { return e.err.Error() }
func (e *permanentMaterializationError) Unwrap() error { return e.err }

type Manager struct {
	config Config
	jobs   batchv1client.BatchV1Interface
	pods   corev1client.PodsGetter
}

func New(
	config Config,
	jobs batchv1client.BatchV1Interface,
	pods corev1client.PodsGetter,
) *Manager {
	return &Manager{config: config, jobs: jobs, pods: pods}
}

// Ensure creates the deterministic Job for an assignment. A matching existing
// Job is success because the Server may deliver the same assignment more than
// once.
func (r *Manager) Ensure(ctx context.Context, assignment api.RunAssignment) (Ensured, error) {
	// Reject invalid assignments before accessing Kubernetes. These failures do
	// not establish that a foreign or replacement Job exists and can finish the Run.
	if !r.config.ownsExecutionNamespace(string(assignment.Placement.JobNamespace)) {
		return Ensured{}, permanentMaterialization(fmt.Errorf(
			"execution namespace %q is not owned by this Controller", assignment.Placement.JobNamespace,
		))
	}
	if assignment.ID == uuid.Nil {
		//nolint:staticcheck // This operator-facing diagnostic begins with the Run domain name.
		return Ensured{}, permanentMaterialization(errors.New("Run ID is required"))
	}
	handle := Handle{
		RunID:     assignment.ID,
		Namespace: string(assignment.Placement.JobNamespace),
		Name:      runJobName(assignment.ID),
	}
	job, err := renderJob(r.config, assignment)
	if err != nil {
		return Ensured{}, permanentMaterialization(err)
	}

	jobs := r.jobs.Jobs(job.Namespace)
	created, err := jobs.Create(ctx, job, metav1.CreateOptions{})
	if err == nil {
		if err := r.validateJobIdentity(handle, created); err != nil {
			return Ensured{}, permanentMaterialization(err)
		}

		return Ensured{
			Handle:  handleFromJob(created, assignment.ID),
			Created: true,
		}, nil
	}
	if !apierrors.IsAlreadyExists(err) {
		return Ensured{}, classifyMaterializationError("create execution Job", err)
	}

	existing, err := jobs.Get(ctx, job.Name, metav1.GetOptions{})
	if err != nil {
		return Ensured{}, classifyMaterializationError("get existing execution Job", err)
	}

	if err := r.validateJobIdentity(handle, existing); err != nil {
		return Ensured{}, permanentMaterialization(err)
	}

	return Ensured{
		Handle: handleFromJob(existing, assignment.ID),
	}, nil
}

func (r *Manager) Recover(ctx context.Context) ([]Handle, error) {
	selector := labels.Set{
		jobpolicy.ManagedByLabel: jobpolicy.ManagedByLabelValue,
		jobpolicy.ClusterLabel:   r.config.ClusterName,
	}.String()
	handles := make([]Handle, 0)
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
			handle := Handle{RunID: runID, Namespace: namespace, Name: runJobName(runID)}
			if err := r.validateJobIdentity(handle, job); err != nil {
				continue
			}

			handles = append(handles, handleFromJob(job, runID))
		}
	}

	return handles, nil
}

func (r *Manager) Observe(ctx context.Context, handle Handle) (Observation, error) {
	if err := r.validateJobHandle(handle); err != nil {
		return Observation{}, err
	}

	job, err := r.jobs.Jobs(handle.Namespace).Get(ctx, handle.Name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			outcome := api.RunObservationOutcomeFailed
			reason := "JobNotFound"
			message := fmt.Sprintf("execution Job %s/%s no longer exists", handle.Namespace, handle.Name)

			return Observation{
				Phase:   api.RunObservationPhaseFinished,
				Outcome: &outcome,
				JobUID:  optionalNonemptyString(handle.UID),
				Reason:  &reason,
				Message: &message,
			}, nil
		}

		return Observation{}, fmt.Errorf("get execution Job state: %w", err)
	}
	if err := r.validateJobIdentity(handle, job); err != nil {
		return Observation{}, err
	}
	handle.UID = string(job.UID)
	uid := new(handle.UID)

	pods, err := r.executionPods(ctx, handle)
	if err != nil {
		return Observation{}, fmt.Errorf("list execution Pods: %w", err)
	}
	terminal := terminalJobObservation(job.Status.Conditions, uid, actionAlreadyTerminated(pods))

	cancellationRequested := false
	cancellationAcknowledged := false
	for index := range pods {
		if pods[index].Annotations[cancellationAnnotation] == "true" {
			cancellationRequested = true
		}
		cancellationAcknowledged = cancellationAcknowledged || podAcknowledgedCancellation(pods[index])
	}
	images, err := observedExecutionImages(pods)
	if err != nil {
		return Observation{}, err
	}
	if terminal != nil {
		terminal.Images = images
	}

	for podIndex := range pods {
		for statusIndex := range pods[podIndex].Status.ContainerStatuses {
			status := &pods[podIndex].Status.ContainerStatuses[statusIndex]
			if status.Name != actionContainerName {
				continue
			}

			if status.State.Running != nil {
				if terminal != nil {
					return canceledIfAcknowledged(*terminal, cancellationRequested, cancellationAcknowledged), nil
				}

				return Observation{
					Phase:  api.RunObservationPhaseRunning,
					JobUID: uid,
					Images: images,
				}, nil
			}
			if status.State.Terminated != nil {
				terminated := status.State.Terminated
				if terminal == nil {
					// A deadline may have fired between the Job and Pod reads. Read
					// its cause after observing termination before trusting the exit code.
					latest, err := r.jobs.Jobs(handle.Namespace).Get(ctx, handle.Name, metav1.GetOptions{})
					if err != nil {
						return Observation{}, fmt.Errorf("get execution Job after Action termination: %w", err)
					}
					if err := r.validateJobIdentity(handle, latest); err != nil {
						return Observation{}, err
					}
					terminal = terminalJobObservation(latest.Status.Conditions, uid, true)
					if terminal != nil {
						terminal.Images = images
					}
				}
				if terminal == nil {
					observation := terminatedObservation(terminated, uid)
					observation.Images = images

					return canceledIfAcknowledged(observation, cancellationRequested, cancellationAcknowledged), nil
				}

				terminal.ExitCode = &terminated.ExitCode
				if terminal.Reason == nil {
					terminal.Reason = optionalNonemptyString(terminated.Reason)
				}
				if terminal.Message == nil {
					terminal.Message = optionalNonemptyString(terminated.Message)
				}

				return canceledIfAcknowledged(*terminal, cancellationRequested, cancellationAcknowledged), nil
			}
		}
	}
	if terminal != nil {
		return canceledIfAcknowledged(*terminal, cancellationRequested, cancellationAcknowledged), nil
	}

	return Observation{
		Phase:  api.RunObservationPhasePreparing,
		JobUID: uid,
		Images: images,
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

func podAcknowledgedCancellation(pod corev1.Pod) bool {
	statuses := make([]corev1.ContainerStatus, 0,
		len(pod.Status.InitContainerStatuses)+len(pod.Status.ContainerStatuses))
	statuses = append(statuses, pod.Status.InitContainerStatuses...)
	statuses = append(statuses, pod.Status.ContainerStatuses...)
	for index := range statuses {
		terminated := statuses[index].State.Terminated
		if terminated != nil && strings.TrimSpace(terminated.Message) == harness.CancellationAcknowledgement {
			return true
		}
	}

	return false
}

func canceledIfAcknowledged(observation Observation, requested, acknowledged bool) Observation {
	if !requested || !acknowledged || !observation.Terminal() {
		return observation
	}
	if (observation.Outcome != nil && *observation.Outcome == api.RunObservationOutcomeTimedOut) ||
		(observation.Reason != nil && *observation.Reason == "PodFailurePolicy") {
		return observation
	}

	outcome := api.RunObservationOutcomeCanceled
	reason := "CancelRequested"
	observation.Outcome = &outcome
	observation.Reason = &reason
	if observation.Message != nil && strings.TrimSpace(*observation.Message) == harness.CancellationAcknowledgement {
		observation.Message = nil
	}

	return observation
}

// Delete removes a completed execution Job only when it still has the UID
// captured when observation began. A missing Job is success so cleanup remains
// idempotent when a deletion response is lost or the Controller restarts.
func (r *Manager) Delete(ctx context.Context, handle Handle) error {
	if err := r.validateJobHandle(handle); err != nil {
		return err
	}
	if handle.UID == "" {
		return fmt.Errorf("%w: delete execution Job requires a pinned UID", ErrRunJobIdentityMismatch)
	}
	job, err := r.jobs.Jobs(handle.Namespace).Get(ctx, handle.Name, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("get completed execution Job before deletion: %w", err)
	}
	if err := r.validateJobIdentity(handle, job); err != nil {
		return err
	}

	uid := types.UID(handle.UID)
	propagation := metav1.DeletePropagationBackground
	options := metav1.DeleteOptions{
		Preconditions:     &metav1.Preconditions{UID: &uid},
		PropagationPolicy: &propagation,
	}
	err = r.jobs.Jobs(handle.Namespace).Delete(ctx, handle.Name, options)
	if err != nil && !apierrors.IsNotFound(err) {
		return fmt.Errorf("delete completed execution Job: %w", err)
	}

	return nil
}

func (r *Manager) OpenLog(ctx context.Context, handle Handle, follow bool) (io.ReadCloser, error) {
	if err := r.validateJobHandle(handle); err != nil {
		return nil, err
	}
	job, err := r.jobs.Jobs(handle.Namespace).Get(ctx, handle.Name, metav1.GetOptions{})
	if err == nil {
		if err := r.validateJobIdentity(handle, job); err != nil {
			return nil, err
		}
		handle.UID = string(job.UID)
	} else if !apierrors.IsNotFound(err) {
		return nil, fmt.Errorf("get execution Job for log stream: %w", err)
	}
	// Original Pods can outlive a deleted Job. Its pinned UID still establishes
	// which remaining log source belongs to this Run.
	if handle.UID == "" {
		return nil, ErrLogNotReady
	}

	pods, err := r.executionPods(ctx, handle)
	if err != nil {
		return nil, fmt.Errorf("list execution Pods for log stream: %w", err)
	}
	if len(pods) == 0 {
		return nil, ErrLogNotReady
	}
	sort.Slice(pods, func(left, right int) bool {
		return pods[left].CreationTimestamp.Before(&pods[right].CreationTimestamp)
	})
	pod := pods[0]
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == actionContainerName &&
			status.State.Waiting != nil {
			return nil, ErrLogNotReady
		}
	}

	stream, err := r.pods.Pods(handle.Namespace).GetLogs(
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

func (r *Manager) validateJobHandle(handle Handle) error {
	if !r.config.ownsExecutionNamespace(handle.Namespace) {
		return fmt.Errorf(
			"%w: execution namespace %q is not owned by this Controller",
			ErrRunJobIdentityMismatch,
			handle.Namespace,
		)
	}
	if handle.RunID == uuid.Nil || handle.Name != runJobName(handle.RunID) {
		return fmt.Errorf("%w: Job name %q does not match Run %s", ErrRunJobIdentityMismatch, handle.Name, handle.RunID)
	}

	return nil
}

// An unbound handle may learn an admitted UID once. A pinned handle must never
// adopt a replacement Job, even when its deterministic name and labels match.
func (r *Manager) validateJobIdentity(handle Handle, job *batchv1.Job) error {
	if err := r.validateJobHandle(handle); err != nil {
		return err
	}
	if job.Namespace != handle.Namespace || job.Name != handle.Name ||
		job.Labels[jobpolicy.ManagedByLabel] != jobpolicy.ManagedByLabelValue ||
		job.Labels[runIDLabel] != handle.RunID.String() ||
		job.Labels[jobpolicy.ClusterLabel] != r.config.ClusterName {
		return fmt.Errorf("%w: Job %s/%s does not have the expected Run identity",
			ErrRunJobIdentityMismatch, job.Namespace, job.Name)
	}
	if job.UID == "" {
		return fmt.Errorf("%w: Job %s/%s has no admitted UID", ErrRunJobIdentityMismatch, job.Namespace, job.Name)
	}
	if handle.UID != "" && string(job.UID) != handle.UID {
		return fmt.Errorf("%w: Job %s/%s UID %q does not match pinned UID %q",
			ErrRunJobIdentityMismatch, job.Namespace, job.Name, job.UID, handle.UID)
	}

	return nil
}

func (r *Manager) executionPods(ctx context.Context, handle Handle) ([]corev1.Pod, error) {
	if handle.UID == "" {
		return nil, fmt.Errorf("%w: select execution Pods requires a pinned Job UID", ErrRunJobIdentityMismatch)
	}
	selector := labels.Set{
		batchv1.JobNameLabel:       handle.Name,
		batchv1.ControllerUidLabel: handle.UID,
		jobpolicy.ManagedByLabel:   jobpolicy.ManagedByLabelValue,
		runIDLabel:                 handle.RunID.String(),
		jobpolicy.ClusterLabel:     r.config.ClusterName,
	}
	podSelector := selector.AsSelector()
	pods, err := r.pods.Pods(handle.Namespace).List(ctx, metav1.ListOptions{LabelSelector: podSelector.String()})
	if err != nil {
		return nil, err
	}
	for index := range pods.Items {
		pod := &pods.Items[index]
		owner := metav1.GetControllerOf(pod)
		if pod.Namespace != handle.Namespace || !podSelector.Matches(labels.Set(pod.Labels)) ||
			owner == nil || owner.APIVersion != "batch/v1" || owner.Kind != "Job" ||
			owner.Name != handle.Name || string(owner.UID) != handle.UID {
			return nil, fmt.Errorf("%w: Pod %s/%s is not controlled by Job %s with UID %q",
				ErrRunJobIdentityMismatch, pod.Namespace, pod.Name, handle.Name, handle.UID)
		}
	}

	return pods.Items, nil
}

func terminalJobObservation(
	conditions []batchv1.JobCondition,
	jobUID *string,
	actionTerminated bool,
) *Observation {
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
		case batchv1.JobFailed, batchv1.JobFailureTarget:
			// Kubernetes publishes FailureTarget before Failed. Once the Action
			// stops, preserve that failure even if it handled termination with exit 0.
			if condition.Type == batchv1.JobFailureTarget && !actionTerminated {
				continue
			}
			if condition.Reason == "DeadlineExceeded" {
				outcome = api.RunObservationOutcomeTimedOut
			}
		default:
			continue
		}

		return &Observation{
			Phase:    api.RunObservationPhaseFinished,
			Outcome:  &outcome,
			JobUID:   jobUID,
			Reason:   optionalNonemptyString(condition.Reason),
			Message:  optionalNonemptyString(condition.Message),
			ExitCode: exitCode,
		}
	}

	return nil
}

func handleFromJob(job *batchv1.Job, runID uuid.UUID) Handle {
	return Handle{
		RunID:     runID,
		Namespace: job.Namespace,
		Name:      job.Name,
		UID:       string(job.UID),
	}
}

func terminatedObservation(terminated *corev1.ContainerStateTerminated, jobUID *string) Observation {
	outcome := api.RunObservationOutcomeFailed
	if terminated.ExitCode == 0 {
		outcome = api.RunObservationOutcomeSucceeded
	}

	return Observation{
		Phase:    api.RunObservationPhaseFinished,
		Outcome:  &outcome,
		JobUID:   jobUID,
		Reason:   optionalNonemptyString(terminated.Reason),
		Message:  optionalNonemptyString(terminated.Message),
		ExitCode: &terminated.ExitCode,
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

func IsPermanentMaterializationError(err error) bool {
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
