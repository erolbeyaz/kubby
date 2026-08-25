{{/* The release's base name, bounded to what Kubernetes accepts. */}}
{{- define "kubby.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "kubby.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else if contains .Chart.Name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name .Chart.Name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "kubby.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{ include "kubby.selectorLabels" . }}
app.kubernetes.io/version: {{ .Values.image.tag | default .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "kubby.selectorLabels" -}}
app.kubernetes.io/name: {{ include "kubby.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "kubby.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "kubby.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
The secret holding the encryption key and the database password.

Refused at render time rather than at run time when neither is given: a deployment that
starts and then crash-loops on a missing key costs somebody an hour of reading logs, and
`helm install` is where the mistake can still be pointed at.
*/}}
{{- define "kubby.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else if .Values.secrets.create -}}
{{- printf "%s-secrets" (include "kubby.fullname" .) -}}
{{- else -}}
{{- fail "set secrets.existingSecret to a Secret holding the encryption key and database password, or secrets.create=true for a first look" -}}
{{- end -}}
{{- end -}}

{{- define "kubby.image" -}}
{{- $tag := .Values.image.tag | default .Chart.AppVersion -}}
{{- printf "%s/%s:%s" .Values.image.registry .Values.image.repository $tag -}}
{{- end -}}
