{{- define "thyris-sz.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "thyris-sz.fullname" -}}
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

{{- define "thyris-sz.chart" -}}
{{- printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "thyris-sz.labels" -}}
helm.sh/chart: {{ include "thyris-sz.chart" . }}
app.kubernetes.io/name: {{ include "thyris-sz.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "thyris-sz.selectorLabels" -}}
app.kubernetes.io/name: {{ include "thyris-sz.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "thyris-sz.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "thyris-sz.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "thyris-sz.secretName" -}}
{{- if .Values.secrets.existingSecret -}}
{{- .Values.secrets.existingSecret -}}
{{- else -}}
{{- printf "%s-secret" (include "thyris-sz.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "thyris-sz.postgresql.fullname" -}}
{{- printf "%s-postgresql" (include "thyris-sz.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "thyris-sz.redis.fullname" -}}
{{- printf "%s-redis" (include "thyris-sz.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "thyris-sz.envoyGateway.extProcName" -}}
{{- printf "%s-ext-proc" (include "thyris-sz.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "thyris-sz.envoyGateway.controllerName" -}}
{{- printf "%s-controller" (include "thyris-sz.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "thyris-sz.envoyGateway.controllerServiceAccountName" -}}
{{- if .Values.envoyGateway.controller.serviceAccount.create -}}
{{- default (include "thyris-sz.envoyGateway.controllerName" .) .Values.envoyGateway.controller.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.envoyGateway.controller.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "thyris-sz.dbDsn" -}}
{{- if .Values.secrets.dbDsn -}}
{{- .Values.secrets.dbDsn -}}
{{- else -}}
{{- printf "postgres://%s:%s@%s:%v/%s?sslmode=disable&TimeZone=Europe/Istanbul" .Values.postgresql.auth.username .Values.postgresql.auth.password (include "thyris-sz.postgresql.fullname" .) .Values.postgresql.service.port .Values.postgresql.auth.database -}}
{{- end -}}
{{- end -}}

{{- define "thyris-sz.redisUrl" -}}
{{- if .Values.secrets.redisUrl -}}
{{- .Values.secrets.redisUrl -}}
{{- else -}}
{{- printf "redis://:%s@%s:%v/0" .Values.redis.auth.password (include "thyris-sz.redis.fullname" .) .Values.redis.service.port -}}
{{- end -}}
{{- end -}}
