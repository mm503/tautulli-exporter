{{/*
Expand the name of the chart.
*/}}
{{- define "tautulli-exporter.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Create a default fully qualified app name.
We truncate at 63 chars because some Kubernetes name fields are limited to this (by the DNS naming spec).
If release name contains chart name it will be used as a full name.
*/}}
{{- define "tautulli-exporter.fullname" -}}
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
Create chart name and version as used by the chart label.
*/}}
{{- define "tautulli-exporter.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- end }}

{{/*
Common labels
*/}}
{{- define "tautulli-exporter.labels" -}}
helm.sh/chart: {{ include "tautulli-exporter.chart" . | quote }}
{{ include "tautulli-exporter.selectorLabels" . }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
app.kubernetes.io/managed-by: {{ .Release.Service | quote }}
{{- end }}

{{/*
Selector labels
*/}}
{{- define "tautulli-exporter.selectorLabels" -}}
app.kubernetes.io/name: {{ include "tautulli-exporter.name" . | quote }}
app.kubernetes.io/instance: {{ .Release.Name | quote }}
{{- end }}

{{/*
Convert a Prometheus duration composed of y, w, d, h, m, s, and ms units to
milliseconds so related ServiceMonitor durations can be compared at render time.

The pattern mirrors Prometheus' own: each unit may appear at most once and units
must run largest to smallest. A looser pattern accepts strings like "30s30s" or
"1s1h" that render fine here but are rejected by the Prometheus Operator, which
turns a typo into a silently broken scrape target instead of a failed install.
*/}}
{{- define "tautulli-exporter.durationMillis" -}}
{{- $duration := toString . -}}
{{- if not (regexMatch "^([0-9]+y)?([0-9]+w)?([0-9]+d)?([0-9]+h)?([0-9]+m)?([0-9]+s)?([0-9]+ms)?$" $duration) -}}
{{- fail (printf "invalid Prometheus duration %q (units must each appear at most once, largest first)" $duration) -}}
{{- end -}}
{{- $total := 0 -}}
{{- range $part := regexFindAll "[0-9]+(ms|[ywdhms])" $duration -1 -}}
{{- $amount := regexFind "^[0-9]+" $part | int64 -}}
{{- $unit := regexFind "(ms|[ywdhms])$" $part -}}
{{- if eq $unit "y" -}}
{{- $total = add $total (mul $amount 31536000000) -}}
{{- else if eq $unit "w" -}}
{{- $total = add $total (mul $amount 604800000) -}}
{{- else if eq $unit "d" -}}
{{- $total = add $total (mul $amount 86400000) -}}
{{- else if eq $unit "h" -}}
{{- $total = add $total (mul $amount 3600000) -}}
{{- else if eq $unit "m" -}}
{{- $total = add $total (mul $amount 60000) -}}
{{- else if eq $unit "s" -}}
{{- $total = add $total (mul $amount 1000) -}}
{{- else -}}
{{- $total = add $total $amount -}}
{{- end -}}
{{- end -}}
{{- if le $total 0 -}}
{{- fail (printf "duration %q must be greater than zero" $duration) -}}
{{- end -}}
{{- $total -}}
{{- end }}
