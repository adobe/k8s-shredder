{{/* vim: set filetype=mustache: */}}
{{/*
Expand the name of the chart.
*/}}
{{- define "k8s-shredder.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "k8s-shredder.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- $name := default .Chart.Name .Values.nameOverride -}}
{{- if contains $name .Release.Name -}}
{{- .Release.Name | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s-%s" .Release.Name $name | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}
{{- end -}}

{{/*
Create chart name and version as used by the chart label.
*/}}
{{- define "k8s-shredder.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels
*/}}
{{- define "k8s-shredder.labels" -}}
app.kubernetes.io/name: {{ include "k8s-shredder.name" . }}
helm.sh/chart: {{ include "k8s-shredder.chart" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- if .Values.podLabels }}
{{ toYaml .Values.podLabels }}
{{- end }}
{{- end -}}

{{/*
matchLabels
*/}}
{{- define "k8s-shredder.matchLabels" -}}
app.kubernetes.io/name: {{ include "k8s-shredder.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Additional pod annotations
*/}}
{{- define "k8s-shredder.annotations" -}}
{{- if .Values.podAnnotations }}
{{- toYaml .Values.podAnnotations }}
{{- end }}
{{- end -}}


{{/*
Create the name of the service account to use.
*/}}
{{- define "k8s-shredder.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
    {{ default (include "k8s-shredder.fullname" .) .Values.serviceAccount.name }}
{{- else -}}
    {{ default "default" .Values.serviceAccount.name }}
{{- end -}}
{{- end -}}

