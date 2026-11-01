{{- define "slipramp-controller.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "slipramp-controller.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "slipramp-controller.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end }}

{{- define "slipramp-controller.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "slipramp-controller.labels" -}}
helm.sh/chart: {{ include "slipramp-controller.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: slipramp
app.kubernetes.io/name: slipramp-controller
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: controller
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
slipramp.com/cluster: {{ .Values.clusterName | quote }}
{{- end }}

{{- define "slipramp-controller.selectorLabels" -}}
app.kubernetes.io/name: slipramp-controller
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/component: controller
{{- end }}

{{- define "slipramp-controller.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- include "slipramp-controller.fullname" . -}}
{{- else -}}
{{- required "serviceAccount.name is required when serviceAccount.create is false" .Values.serviceAccount.name -}}
{{- end -}}
{{- end }}

{{- define "slipramp-controller.managedSecretName" -}}
{{- printf "%s-credentials" (include "slipramp-controller.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "slipramp-controller.image" -}}
{{- $tag := default .appVersion .image.tag -}}
{{- if eq $tag "latest" -}}
{{- fail "image tag must not be latest" -}}
{{- end -}}
{{- if .image.digest -}}
{{- printf "%s:%s@%s" .image.repository $tag .image.digest -}}
{{- else -}}
{{- printf "%s:%s" .image.repository $tag -}}
{{- end -}}
{{- end }}

{{- define "slipramp-controller.secretName" -}}
{{- if .secret.value -}}
{{- include "slipramp-controller.managedSecretName" .root -}}
{{- else -}}
{{- required "an existing Secret name is required when an inline secret value is not set" .secret.existingSecret.name -}}
{{- end -}}
{{- end }}

{{- define "slipramp-controller.secretKey" -}}
{{- if .secret.value -}}
{{- .managedKey -}}
{{- else -}}
{{- required "an existing Secret key is required when an inline secret value is not set" .secret.existingSecret.key -}}
{{- end -}}
{{- end }}
