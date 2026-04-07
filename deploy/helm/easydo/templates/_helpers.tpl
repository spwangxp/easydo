{{- define "easydo.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "easydo.fullname" -}}
{{- if .Values.fullnameOverride -}}
{{- .Values.fullnameOverride | trunc 63 | trimSuffix "-" -}}
{{- else -}}
{{- printf "%s" (include "easydo.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}
{{- end -}}

{{- define "easydo.labels" -}}
app.kubernetes.io/name: {{ include "easydo.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | quote }}
{{- end -}}

{{- define "easydo.selectorLabels" -}}
app.kubernetes.io/name: {{ include "easydo.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "easydo.serverFullname" -}}
{{- printf "%s-server" (include "easydo.fullname" .) -}}
{{- end -}}

{{- define "easydo.frontendFullname" -}}
{{- printf "%s-frontend" (include "easydo.fullname" .) -}}
{{- end -}}

{{- define "easydo.agentFullname" -}}
{{- printf "%s-agent" (include "easydo.fullname" .) -}}
{{- end -}}

{{- define "easydo.mariadbFullname" -}}
{{- printf "%s-mariadb" (include "easydo.fullname" .) -}}
{{- end -}}

{{- define "easydo.redisFullname" -}}
{{- printf "%s-redis" (include "easydo.fullname" .) -}}
{{- end -}}

{{- define "easydo.minioFullname" -}}
{{- printf "%s-minio" (include "easydo.fullname" .) -}}
{{- end -}}
