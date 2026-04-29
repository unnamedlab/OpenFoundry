{{/*
Expand the name of the chart.
*/}}
{{- define "iceberg-catalog.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name. Honours `fullnameOverride` and respects the
parent release name when installed as a subchart.
*/}}
{{- define "iceberg-catalog.fullname" -}}
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
Common labels.
*/}}
{{- define "iceberg-catalog.labels" -}}
app.kubernetes.io/name: {{ include "iceberg-catalog.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: iceberg-catalog
app.kubernetes.io/part-of: openfoundry
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end -}}

{{/*
Selector labels.
*/}}
{{- define "iceberg-catalog.selectorLabels" -}}
app.kubernetes.io/name: {{ include "iceberg-catalog.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{/*
Service account name.
*/}}
{{- define "iceberg-catalog.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "iceberg-catalog.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Name of the Secret holding the realm root client secret.
*/}}
{{- define "iceberg-catalog.realmSecretName" -}}
{{- if .Values.realm.existingSecret -}}
{{- .Values.realm.existingSecret -}}
{{- else -}}
{{- printf "%s-realm" (include "iceberg-catalog.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Name of the Secret holding the Postgres password.
*/}}
{{- define "iceberg-catalog.postgresSecretName" -}}
{{- if .Values.postgres.existingSecret -}}
{{- .Values.postgres.existingSecret -}}
{{- else -}}
{{- printf "%s-postgres" (include "iceberg-catalog.fullname" .) -}}
{{- end -}}
{{- end -}}

{{/*
Name of the CloudNativePG cluster resource (when enabled).
*/}}
{{- define "iceberg-catalog.cnpgClusterName" -}}
{{- printf "%s-pg" (include "iceberg-catalog.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Resolve the JDBC URL for Polaris. Prefers an explicit external URL,
falls back to host/port composition, or the CloudNativePG `-rw` service.
*/}}
{{- define "iceberg-catalog.jdbcUrl" -}}
{{- if .Values.postgres.cnpg.enabled -}}
{{- printf "jdbc:postgresql://%s-rw:5432/%s" (include "iceberg-catalog.cnpgClusterName" .) .Values.postgres.database -}}
{{- else if .Values.postgres.external.jdbcUrl -}}
{{- .Values.postgres.external.jdbcUrl -}}
{{- else -}}
{{- printf "jdbc:postgresql://%s:%v/%s" .Values.postgres.external.host .Values.postgres.external.port .Values.postgres.database -}}
{{- end -}}
{{- end -}}
