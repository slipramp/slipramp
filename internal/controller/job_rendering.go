package controller

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"math"
	"path"
	"slices"
	"strconv"
	"time"

	"github.com/google/uuid"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/harness"
)

const (
	actionContainerName         = "action"
	checkoutContainerName       = "checkout"
	harnessContainerName        = "install-harness"
	workspaceVolumeName         = "slipramp-workspace"
	harnessVolumeName           = "slipramp-bin"
	runStateVolumeName          = "slipramp-run-state"
	artifactWorkspaceVolumeName = "slipramp-artifact-workspace"
	gitCredentialVolumeName     = "slipramp-git-credential"
	checkoutWorkspaceMountPath  = "/slipramp/checkout"
	harnessMountPath            = "/slipramp/bin"
	harnessExecutablePath       = harnessMountPath + "/slipramp-harness"
	runStateMountPath           = "/slipramp/run-state"
	artifactWorkspaceMountPath  = "/slipramp"
	cancellationFileName        = "cancellation-requested"
	cancellationFilePath        = runStateMountPath + "/" + cancellationFileName
	gitCredentialMountPath      = "/slipramp/git-credential"
	defaultRuntimeWorkspacePath = "/workspace"
	managedByLabel              = "app.kubernetes.io/managed-by"
	managedByLabelValue         = "slipramp-controller"
	runIDLabel                  = "slipramp.com/run-id"
	clusterLabel                = "slipramp.com/cluster"
	cancellationAnnotation      = "slipramp.com/cancellation-requested"
)

//go:embed scripts/checkout.sh
var checkoutScript string

func renderJob(config Config, assignment api.RunAssignment) (*batchv1.Job, error) {
	if string(assignment.Placement.Cluster) != config.ClusterName {
		return nil, fmt.Errorf(
			"run assignment cluster %q does not match controller cluster %q",
			assignment.Placement.Cluster,
			config.ClusterName,
		)
	}

	template, err := assignmentPodTemplate(assignment.Placement.PodTemplate)
	if err != nil {
		return nil, err
	}

	labels := map[string]string{
		managedByLabel: managedByLabelValue,
		runIDLabel:     assignment.ID.String(),
		clusterLabel:   config.ClusterName,
	}
	if template.Labels == nil {
		template.Labels = map[string]string{}
	}
	maps.Copy(template.Labels, labels)
	if template.Annotations == nil {
		template.Annotations = map[string]string{}
	}
	template.Annotations[cancellationAnnotation] = "false"

	volumes := []corev1.Volume{
		{
			Name:     workspaceVolumeName,
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
		{
			Name:     harnessVolumeName,
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
		{
			Name:     artifactWorkspaceVolumeName,
			EmptyDir: &corev1.EmptyDirVolumeSource{},
		},
		{
			Name: runStateVolumeName,
			DownwardAPI: &corev1.DownwardAPIVolumeSource{
				Items: []corev1.DownwardAPIVolumeFile{
					{
						Path: cancellationFileName,
						FieldRef: &corev1.ObjectFieldSelector{
							FieldPath: "metadata.annotations['" +
								cancellationAnnotation + "']",
						},
					},
				},
			},
		},
	}

	checkout, checkoutVolumes, err := checkoutContainer(config, assignment)
	if err != nil {
		return nil, err
	}
	volumes = append(volumes, checkoutVolumes...)

	action, credentialVolumes, err := actionContainer(config, assignment)
	if err != nil {
		return nil, err
	}
	volumes = append(volumes, credentialVolumes...)

	if err := appendOwnedVolumes(&template.Spec, volumes); err != nil {
		return nil, err
	}

	template.Spec.InitContainers = []corev1.Container{
		{
			Name:  harnessContainerName,
			Image: config.HarnessImage,
			Args:  []string{"--install", harnessExecutablePath},
			VolumeMounts: []corev1.VolumeMount{
				{Name: harnessVolumeName, MountPath: harnessMountPath},
			},
		},
		checkout,
	}
	template.Spec.Containers = []corev1.Container{action}
	template.Spec.RestartPolicy = corev1.RestartPolicyNever
	template.Spec.ServiceAccountName = valueOrEmpty(
		assignment.Placement.ServiceAccountName,
	)
	template.Spec.NodeSelector = cloneOptionalMap(
		assignment.Placement.NodeSelector,
	)

	deadline, err := assignmentDeadline(assignment)
	if err != nil {
		return nil, err
	}

	backoffLimit := int32(0)
	jobName := runJobName(assignment.ID)

	return &batchv1.Job{
		APIVersion: "batch/v1",
		Kind:       "Job",
		Name:       jobName,
		Namespace:  string(assignment.Placement.JobNamespace),
		Labels:     maps.Clone(labels),
		Spec: batchv1.JobSpec{
			BackoffLimit:          &backoffLimit,
			ActiveDeadlineSeconds: deadline,
			Template:              template,
		},
	}, nil
}

func runJobName(runID uuid.UUID) string {
	return "slipramp-run-" + runID.String()
}

func assignmentPodTemplate(
	encoded *map[string]any,
) (corev1.PodTemplateSpec, error) {
	if encoded == nil {
		return corev1.PodTemplateSpec{}, nil
	}

	data, err := json.Marshal(encoded)
	if err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf(
			"encode assignment pod template: %w",
			err,
		)
	}

	var template corev1.PodTemplateSpec
	if err := json.Unmarshal(data, &template); err != nil {
		return corev1.PodTemplateSpec{}, fmt.Errorf(
			"decode assignment pod template: %w",
			err,
		)
	}

	if len(template.Spec.Containers) != 0 ||
		len(template.Spec.InitContainers) != 0 {
		return corev1.PodTemplateSpec{}, fmt.Errorf(
			"assignment pod template must not define containers",
		)
	}

	return template, nil
}

func checkoutContainer(
	config Config,
	assignment api.RunAssignment,
) (corev1.Container, []corev1.Volume, error) {
	credentialPath := ""
	volumes := []corev1.Volume{}
	volumeMounts := []corev1.VolumeMount{
		{Name: artifactWorkspaceVolumeName, MountPath: artifactWorkspaceMountPath},
		{Name: workspaceVolumeName, MountPath: checkoutWorkspaceMountPath},
		{Name: harnessVolumeName, MountPath: harnessMountPath, ReadOnly: true},
		{Name: runStateVolumeName, MountPath: runStateMountPath, ReadOnly: true},
	}

	if assignment.Repository.CheckoutCredentialName != nil {
		credentialName := string(*assignment.Repository.CheckoutCredentialName)
		secretName, found := config.GitCredentials[credentialName]
		if !found {
			return corev1.Container{}, nil, fmt.Errorf(
				"git checkout credential %q is not configured for cluster %q",
				credentialName,
				config.ClusterName,
			)
		}

		mode := int32(0o400)
		volumes = append(volumes, corev1.Volume{
			Name: gitCredentialVolumeName,
			Secret: &corev1.SecretVolumeSource{
				SecretName:  secretName,
				DefaultMode: &mode,
			},
		})
		volumeMounts = append(volumeMounts, corev1.VolumeMount{
			Name:      gitCredentialVolumeName,
			MountPath: gitCredentialMountPath,
			ReadOnly:  true,
		})
		credentialPath = gitCredentialMountPath
	}

	return corev1.Container{
		Name:    checkoutContainerName,
		Image:   config.CheckoutImage,
		Command: []string{harnessExecutablePath},
		Args: []string{
			"/bin/sh",
			"-ec",
			checkoutScript,
			"slipramp-checkout",
			checkoutWorkspaceMountPath,
			assignment.Repository.CloneURL,
			assignment.CommitSha,
			credentialPath,
		},
		Env: []corev1.EnvVar{
			{
				Name:  harness.CancellationFileEnvironment,
				Value: cancellationFilePath,
			},
		},
		VolumeMounts: volumeMounts,
	}, volumes, nil
}

func actionContainer(
	config Config,
	assignment api.RunAssignment,
) (corev1.Container, []corev1.Volume, error) {
	arguments := append([]string(nil), assignment.Action.Command...)
	environment := []corev1.EnvVar{
		{
			Name:  harness.CancellationFileEnvironment,
			Value: cancellationFilePath,
		},
	}
	artifactEnvironment, err := assignmentArtifactEnvironment(config, assignment)
	if err != nil {
		return corev1.Container{}, nil, err
	}
	environment = append(environment, artifactEnvironment...)

	if assignment.Runtime.Environment != nil {
		for _, name := range slices.Sorted(maps.Keys(*assignment.Runtime.Environment)) {
			environment = append(environment, corev1.EnvVar{
				Name:  name,
				Value: (*assignment.Runtime.Environment)[name],
			})
		}
	}

	parameterEnvironment, parameterArguments, err := assignmentParameters(assignment)
	if err != nil {
		return corev1.Container{}, nil, err
	}
	environment = append(environment, parameterEnvironment...)
	arguments = append(arguments, parameterArguments...)

	credentials, err := assignmentCredentials(assignment)
	if err != nil {
		return corev1.Container{}, nil, err
	}
	environment = append(environment, credentials.environment...)

	workspacePath := defaultRuntimeWorkspacePath
	if assignment.Runtime.WorkingDirectory != nil {
		workspacePath = *assignment.Runtime.WorkingDirectory
	}
	workingDirectory := workspacePath
	if assignment.Target.Path != nil {
		workingDirectory = path.Join(workspacePath, *assignment.Target.Path)
	}

	resources, err := assignmentResources(assignment)
	if err != nil {
		return corev1.Container{}, nil, err
	}

	volumeMounts := append(
		[]corev1.VolumeMount{
			{Name: artifactWorkspaceVolumeName, MountPath: artifactWorkspaceMountPath},
			{Name: workspaceVolumeName, MountPath: workspacePath},
			{Name: harnessVolumeName, MountPath: harnessMountPath, ReadOnly: true},
			{Name: runStateVolumeName, MountPath: runStateMountPath, ReadOnly: true},
		},
		credentials.volumeMounts...,
	)

	return corev1.Container{
		Name:         actionContainerName,
		Image:        assignment.Runtime.Image,
		Command:      []string{harnessExecutablePath},
		Args:         arguments,
		WorkingDir:   workingDirectory,
		Env:          environment,
		EnvFrom:      credentials.environmentFrom,
		Resources:    resources,
		VolumeMounts: volumeMounts,
	}, credentials.volumes, nil
}

func assignmentArtifactEnvironment(
	config Config,
	assignment api.RunAssignment,
) ([]corev1.EnvVar, error) {
	artifacts, err := json.Marshal(assignment.Action.Artifacts)
	if err != nil {
		return nil, fmt.Errorf("encode artifact declarations: %w", err)
	}
	inputs, err := json.Marshal(assignment.Action.Inputs)
	if err != nil {
		return nil, fmt.Errorf("encode artifact input declarations: %w", err)
	}

	return []corev1.EnvVar{
		{Name: harness.ServerURLEnvironment, Value: config.ServerURL},
		{Name: harness.RunIDEnvironment, Value: assignment.ID.String()},
		{Name: harness.ArtifactTokenEnvironment, Value: assignment.ArtifactAccess.Token},
		{Name: harness.ArtifactsEnvironment, Value: string(artifacts)},
		{Name: harness.ArtifactInputsEnvironment, Value: string(inputs)},
	}, nil
}

func assignmentParameters(
	assignment api.RunAssignment,
) ([]corev1.EnvVar, []string, error) {
	environment := []corev1.EnvVar{}
	arguments := []string{}

	for _, name := range slices.Sorted(maps.Keys(assignment.Parameters)) {
		if assignment.Action.ParameterDefinitions == nil {
			return nil, nil, fmt.Errorf(
				"run parameter %q has no definition",
				name,
			)
		}

		definition, found := (*assignment.Action.ParameterDefinitions)[name]
		if !found {
			return nil, nil, fmt.Errorf(
				"run parameter %q has no definition",
				name,
			)
		}

		value, boolean, err := parameterValue(definition.Type, assignment.Parameters[name])
		if err != nil {
			return nil, nil, fmt.Errorf("render run parameter %q: %w", name, err)
		}

		if definition.Environment != nil {
			environment = append(environment, corev1.EnvVar{
				Name:  *definition.Environment,
				Value: value,
			})
			continue
		}

		if definition.Argument == nil {
			return nil, nil, fmt.Errorf(
				"run parameter %q has no execution mapping",
				name,
			)
		}

		if definition.Argument.Name == nil {
			arguments = append(arguments, value)
			continue
		}

		if definition.Type == api.ParameterTypeBoolean {
			if boolean {
				arguments = append(arguments, *definition.Argument.Name)
			}
			continue
		}

		arguments = append(arguments, *definition.Argument.Name, value)
	}

	return environment, arguments, nil
}

func parameterValue(
	parameterType api.ParameterType,
	value api.ParameterValue,
) (string, bool, error) {
	switch parameterType {
	case api.ParameterTypeString, api.ParameterTypeEnum:
		text, err := value.AsParameterValue0()
		return text, false, err
	case api.ParameterTypeBoolean:
		boolean, err := value.AsParameterValue1()
		return strconv.FormatBool(boolean), boolean, err
	case api.ParameterTypeNumber:
		number, err := value.AsParameterValue2()
		return strconv.FormatFloat(number, 'f', -1, 64), false, err
	default:
		return "", false, fmt.Errorf("unsupported parameter type %q", parameterType)
	}
}

type renderedCredentials struct {
	environment     []corev1.EnvVar
	environmentFrom []corev1.EnvFromSource
	volumeMounts    []corev1.VolumeMount
	volumes         []corev1.Volume
}

func assignmentCredentials(
	assignment api.RunAssignment,
) (renderedCredentials, error) {
	if assignment.Runtime.Credentials == nil {
		return renderedCredentials{}, nil
	}

	result := renderedCredentials{}

	credentialNames := slices.Sorted(maps.Keys(*assignment.Runtime.Credentials))
	for index, name := range credentialNames {
		binding, credential, err := boundCredential(assignment, name)
		if err != nil {
			return renderedCredentials{}, err
		}
		injection := credential.Inject
		secretName := binding.SecretName

		if injection.Environment != nil {
			for _, environmentName := range slices.Sorted(
				maps.Keys(*injection.Environment),
			) {
				variable := (*injection.Environment)[environmentName]
				result.environment = append(result.environment, corev1.EnvVar{
					Name: environmentName,
					ValueFrom: &corev1.EnvVarSource{
						SecretKeyRef: &corev1.SecretKeySelector{
							Name: secretName,
							Key:  variable.Key,
						},
					},
				})
			}
		}

		if injection.EnvironmentFrom != nil && *injection.EnvironmentFrom {
			result.environmentFrom = append(
				result.environmentFrom,
				corev1.EnvFromSource{
					SecretRef: &corev1.SecretEnvSource{
						Name: secretName,
					},
				},
			)
		}

		if injection.Files != nil && len(*injection.Files) > 0 {
			volumeName := "slipramp-credential-" + strconv.Itoa(index) + "-files"
			items := make([]corev1.KeyToPath, 0, len(*injection.Files))
			for fileIndex, file := range *injection.Files {
				itemPath := "file-" + strconv.Itoa(fileIndex)
				items = append(items, corev1.KeyToPath{
					Key:  file.Key,
					Path: itemPath,
					Mode: mode32(file.Mode),
				})
				result.volumeMounts = append(result.volumeMounts, corev1.VolumeMount{
					Name:      volumeName,
					MountPath: file.Path,
					SubPath:   itemPath,
					ReadOnly:  true,
				})
			}
			result.volumes = append(result.volumes, corev1.Volume{
				Name: volumeName,
				Secret: &corev1.SecretVolumeSource{
					SecretName: secretName,
					Items:      items,
				},
			})
		}

		if injection.Directory != nil {
			volumeName := "slipramp-credential-" + strconv.Itoa(index) + "-directory"
			result.volumes = append(result.volumes, corev1.Volume{
				Name: volumeName,
				Secret: &corev1.SecretVolumeSource{
					SecretName:  secretName,
					DefaultMode: mode32(injection.Directory.Mode),
				},
			})
			result.volumeMounts = append(result.volumeMounts, corev1.VolumeMount{
				Name:      volumeName,
				MountPath: injection.Directory.Path,
				ReadOnly:  true,
			})
		}
	}

	return result, nil
}

func boundCredential(
	assignment api.RunAssignment,
	name string,
) (api.RunAssignmentCredential, api.RuntimeCredential, error) {
	credential := (*assignment.Runtime.Credentials)[name]
	if assignment.Action.Credentials == nil {
		return api.RunAssignmentCredential{}, api.RuntimeCredential{}, fmt.Errorf(
			"runtime credential %q has no action binding",
			name,
		)
	}

	sourceName, found := (*assignment.Action.Credentials)[name]
	if !found || assignment.Placement.Credentials == nil {
		return api.RunAssignmentCredential{}, api.RuntimeCredential{}, fmt.Errorf(
			"runtime credential %q has no placement source",
			name,
		)
	}

	binding, found := (*assignment.Placement.Credentials)[string(sourceName)]
	if !found {
		return api.RunAssignmentCredential{}, api.RuntimeCredential{}, fmt.Errorf(
			"runtime credential %q references unavailable placement source %q",
			name,
			sourceName,
		)
	}

	return binding, credential, nil
}

func assignmentResources(
	assignment api.RunAssignment,
) (corev1.ResourceRequirements, error) {
	requests := map[string]string{}
	limits := map[string]string{}
	mergeResources(requests, limits, assignment.Placement.Resources)
	mergeResources(requests, limits, assignment.Action.Resources)

	parsedRequests, err := parseResourceList("request", requests)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}
	parsedLimits, err := parseResourceList("limit", limits)
	if err != nil {
		return corev1.ResourceRequirements{}, err
	}

	return corev1.ResourceRequirements{
		Requests: parsedRequests,
		Limits:   parsedLimits,
	}, nil
}

func mergeResources(
	requests map[string]string,
	limits map[string]string,
	resources *api.ResourceRequirements,
) {
	if resources == nil {
		return
	}
	if resources.Requests != nil {
		maps.Copy(requests, *resources.Requests)
	}
	if resources.Limits != nil {
		maps.Copy(limits, *resources.Limits)
	}
}

func parseResourceList(
	kind string,
	values map[string]string,
) (corev1.ResourceList, error) {
	if len(values) == 0 {
		return nil, nil
	}

	result := make(corev1.ResourceList, len(values))
	for _, name := range slices.Sorted(maps.Keys(values)) {
		quantity, err := resource.ParseQuantity(values[name])
		if err != nil {
			return nil, fmt.Errorf(
				"parse resource %s %q: %w",
				kind,
				name,
				err,
			)
		}
		result[corev1.ResourceName(name)] = quantity
	}

	return result, nil
}

func assignmentDeadline(assignment api.RunAssignment) (*int64, error) {
	timeout := assignment.Placement.Timeout
	if assignment.Action.Timeout != nil {
		timeout = assignment.Action.Timeout
	}
	if timeout == nil {
		return nil, nil
	}

	duration, err := time.ParseDuration(*timeout)
	if err != nil {
		return nil, fmt.Errorf("parse run timeout: %w", err)
	}
	seconds := int64(math.Ceil(duration.Seconds()))
	if seconds < 1 {
		return nil, fmt.Errorf("run timeout must be positive")
	}

	return &seconds, nil
}

func appendOwnedVolumes(spec *corev1.PodSpec, volumes []corev1.Volume) error {
	existing := make(map[string]struct{}, len(spec.Volumes))
	for _, volume := range spec.Volumes {
		existing[volume.Name] = struct{}{}
	}

	for _, volume := range volumes {
		if _, found := existing[volume.Name]; found {
			return fmt.Errorf(
				"assignment pod template volume %q conflicts with a Slipramp volume",
				volume.Name,
			)
		}
		existing[volume.Name] = struct{}{}
		spec.Volumes = append(spec.Volumes, volume)
	}

	return nil
}

func valueOrEmpty(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func cloneOptionalMap(value *map[string]string) map[string]string {
	if value == nil {
		return nil
	}

	return maps.Clone(*value)
}

func mode32(value *uint32) *int32 {
	if value == nil {
		return nil
	}

	mode := int32(*value)

	return &mode
}
