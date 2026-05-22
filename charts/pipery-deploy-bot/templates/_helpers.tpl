{{- define "pipery-deploy-bot.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "pipery-deploy-bot.fullname" -}}
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

{{- define "pipery-deploy-bot.labels" -}}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | quote }}
app.kubernetes.io/name: {{ include "pipery-deploy-bot.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
{{- end -}}

{{- define "pipery-deploy-bot.selectorLabels" -}}
app.kubernetes.io/name: {{ include "pipery-deploy-bot.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "pipery-deploy-bot.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "pipery-deploy-bot.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{- define "pipery-deploy-bot.configName" -}}
{{- default (printf "%s-config" (include "pipery-deploy-bot.fullname" .)) .Values.config.existingConfigMap -}}
{{- end -}}

{{- define "pipery-deploy-bot.privateKeySecretName" -}}
{{- default (printf "%s-private-key" (include "pipery-deploy-bot.fullname" .)) .Values.privateKey.existingSecret -}}
{{- end -}}

{{- define "pipery-deploy-bot.apiTokenSecretName" -}}
{{- default (printf "%s-api-token" (include "pipery-deploy-bot.fullname" .)) .Values.apiToken.existingSecret -}}
{{- end -}}

{{- define "pipery-deploy-bot.databaseSecretName" -}}
{{- default (printf "%s-database" (include "pipery-deploy-bot.fullname" .)) .Values.database.existingSecret -}}
{{- end -}}
