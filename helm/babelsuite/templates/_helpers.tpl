{{/*
Expand the name of the chart.
*/}}
{{- define "babelsuite.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
*/}}
{{- define "babelsuite.fullname" -}}
{{- if .Values.fullnameOverride }}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- $name := default .Chart.Name .Values.nameOverride }}
{{- if contains $name .Release.Name }}
{{- .Release.Name | trunc 63 | trimSuffix "-" }}
{{- else }}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" }}
{{- end }}
{{- end }}
{{- end }}

{{/*
Create chart label.
*/}}
{{- define "babelsuite.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels.
*/}}
{{- define "babelsuite.labels" -}}
helm.sh/chart: {{ include "babelsuite.chart" . }}
{{ include "babelsuite.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end }}

{{/*
Selector labels.
*/}}
{{- define "babelsuite.selectorLabels" -}}
app.kubernetes.io/name: {{ include "babelsuite.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end }}

{{/*
ServiceAccount name.
*/}}
{{- define "babelsuite.serviceAccountName" -}}
{{- if .Values.serviceAccount.create }}
{{- default (include "babelsuite.fullname" .) .Values.serviceAccount.name }}
{{- else }}
{{- default "default" .Values.serviceAccount.name }}
{{- end }}
{{- end }}

{{/*
Secret name — prefer existingSecret, fall back to release-scoped secret.
*/}}
{{- define "babelsuite.secretName" -}}
{{- if .Values.secrets.existingSecret }}
{{- .Values.secrets.existingSecret }}
{{- else }}
{{- include "babelsuite.fullname" . }}
{{- end }}
{{- end }}

{{/*
The replica count, after checking that more than one was asked for on purpose.
A BabelSuite control plane keeps its executions, queue and live streams in
memory, so two replicas do not share work. Autoscaling would start more.
*/}}
{{- define "babelsuite.singleControlPlane" -}}
{{- if and (or (gt (int .Values.replicaCount) 1) .Values.autoscaling.enabled) (not .Values.allowMultipleReplicas) -}}
{{- fail "BabelSuite runs one control plane: an execution lives in the memory of the process that started it, so replicaCount > 1 and autoscaling.enabled would split runs between replicas that cannot see each other. Set allowMultipleReplicas=true only if you accept that (see docs/operations.md)." -}}
{{- end -}}
{{- .Values.replicaCount -}}
{{- end -}}
