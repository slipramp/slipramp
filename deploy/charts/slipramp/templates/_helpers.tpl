{{- define "slipramp.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "slipramp.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := include "slipramp.name" . -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end }}

{{- define "slipramp.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "slipramp.labels" -}}
helm.sh/chart: {{ include "slipramp.chart" . }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/part-of: slipramp
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}

{{- define "slipramp.selectorLabels" -}}
app.kubernetes.io/name: {{ include "slipramp.name" .root }}
app.kubernetes.io/instance: {{ .root.Release.Name }}
app.kubernetes.io/component: {{ .component }}
{{- end }}

{{- define "slipramp.serverName" -}}
{{- printf "%s-server" (include "slipramp.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "slipramp.webName" -}}
{{- printf "%s-web" (include "slipramp.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "slipramp.managedSecretName" -}}
{{- printf "%s-secrets" (include "slipramp.serverName" .) | trunc 63 | trimSuffix "-" -}}
{{- end }}

{{- define "slipramp.image" -}}
{{- if eq .tag "latest" -}}
{{- fail "image tag must not be latest" -}}
{{- end -}}
{{- printf "%s:%s" .repository .tag -}}
{{- end }}

{{- define "slipramp.secretName" -}}
{{- if .secret.value -}}
{{- include "slipramp.managedSecretName" .root -}}
{{- else -}}
{{- required "an existing Secret name is required when an inline secret value is not set" .secret.existingSecret.name -}}
{{- end -}}
{{- end }}

{{- define "slipramp.secretKey" -}}
{{- if .secret.value -}}
{{- .managedKey -}}
{{- else -}}
{{- required "an existing Secret key is required when an inline secret value is not set" .secret.existingSecret.key -}}
{{- end -}}
{{- end }}
