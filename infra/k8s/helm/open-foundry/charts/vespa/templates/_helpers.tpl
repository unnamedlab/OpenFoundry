{{/*
Expand the name of the chart.
*/}}
{{- define "vespa.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Fully qualified app name. Honours `fullnameOverride` and respects the
parent release name when installed as a subchart.
*/}}
{{- define "vespa.fullname" -}}
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
Per-role names.  Kept short enough that the StatefulSet pod names plus
the headless service DNS still fit Vespa's 63-char hostname limit.
*/}}
{{- define "vespa.configserver.fullname" -}}
{{- printf "%s-configserver" (include "vespa.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "vespa.content.fullname" -}}
{{- printf "%s-content" (include "vespa.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "vespa.container.fullname" -}}
{{- printf "%s-container" (include "vespa.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "vespa.deploy.fullname" -}}
{{- printf "%s-deploy" (include "vespa.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "vespa.appPackage.configMapName" -}}
{{- printf "%s-app" (include "vespa.fullname" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{/*
Common labels.
*/}}
{{- define "vespa.labels" -}}
app.kubernetes.io/name: {{ include "vespa.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
app.kubernetes.io/component: vespa
app.kubernetes.io/part-of: openfoundry
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" | trunc 63 | trimSuffix "-" }}
{{- if .Chart.AppVersion }}
app.kubernetes.io/version: {{ .Chart.AppVersion | quote }}
{{- end }}
{{- end -}}

{{/*
Selector labels: per-role so each StatefulSet only matches its own pods.
*/}}
{{- define "vespa.selectorLabels" -}}
app.kubernetes.io/name: {{ include "vespa.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}

{{- define "vespa.configserver.selectorLabels" -}}
{{ include "vespa.selectorLabels" . }}
app.kubernetes.io/component: vespa-configserver
{{- end -}}

{{- define "vespa.content.selectorLabels" -}}
{{ include "vespa.selectorLabels" . }}
app.kubernetes.io/component: vespa-content
{{- end -}}

{{- define "vespa.container.selectorLabels" -}}
{{ include "vespa.selectorLabels" . }}
app.kubernetes.io/component: vespa-container
{{- end -}}

{{/*
Service account name.
*/}}
{{- define "vespa.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "vespa.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
Comma-separated list of configserver hostnames, used by every pod via
the `VESPA_CONFIGSERVERS` env var (Vespa's standard discovery hook).
*/}}
{{- define "vespa.configservers" -}}
{{- $cs := include "vespa.configserver.fullname" . -}}
{{- $ns := .Release.Namespace -}}
{{- $count := int .Values.configserver.replicas -}}
{{- $hosts := list -}}
{{- range $i, $_ := until $count -}}
{{- $hosts = append $hosts (printf "%s-%d.%s.%s.svc.cluster.local" $cs $i $cs $ns) -}}
{{- end -}}
{{- join "," $hosts -}}
{{- end -}}

{{/*
URL for the first configserver — used by the deploy Job which only needs
to talk to one node (the configservers replicate the package between
themselves).
*/}}
{{- define "vespa.configserver.url" -}}
{{- printf "http://%s-0.%s.%s.svc.cluster.local:19071" (include "vespa.configserver.fullname" .) (include "vespa.configserver.fullname" .) .Release.Namespace -}}
{{- end -}}

{{/*
Render the application-package files into a map suitable for embedding
in a ConfigMap's `data:` block.

  * `services.xml` and every `files/schemas/*.sd` are taken verbatim from
    the chart's `files/` directory (the canonical copy lives at
    `infra/k8s/vespa/app/`; the chart mirrors it).
  * `hosts.xml` is rendered from `files/hosts.xml.tmpl` *or* fully
    overridden via `.Values.applicationPackage.hostsXmlOverride`.
  * `.Values.applicationPackage.extraFiles` are appended last and may
    overlay any of the above.

The keys use `_` instead of `/` because ConfigMap keys cannot contain
slashes; the deploy Job rewrites them back into a directory tree before
zipping.
*/}}
{{- define "vespa.appPackage.data" -}}
{{- $data := dict -}}
{{- $_ := set $data "services.xml" (.Files.Get "files/services.xml") -}}
{{- if .Values.applicationPackage.hostsXmlOverride -}}
{{- $_ := set $data "hosts.xml" .Values.applicationPackage.hostsXmlOverride -}}
{{- else -}}
{{- $_ := set $data "hosts.xml" (include "vespa.hostsXml" .) -}}
{{- end -}}
{{- range $path, $_bytes := .Files.Glob "files/schemas/*.sd" -}}
{{- $key := printf "schemas_%s" (base $path) -}}
{{- $_ := set $data $key ($.Files.Get $path) -}}
{{- end -}}
{{- range $path, $content := .Values.applicationPackage.extraFiles -}}
{{- $key := $path | replace "/" "_" -}}
{{- $_ := set $data $key $content -}}
{{- end -}}
{{- toYaml $data -}}
{{- end -}}

{{/*
Auto-generate `hosts.xml` from the StatefulSet pod-DNS names.

Mapping (must stay in sync with `services.xml`):

   vespa-configserver-N → <release>-vespa-configserver-N.…
   vespa-container-N    → <release>-vespa-container-N.…
   vespa-content-N      → <release>-vespa-content-N.…
*/}}
{{- define "vespa.hostsXml" -}}
{{- $ns := .Release.Namespace -}}
{{- $cs := include "vespa.configserver.fullname" . -}}
{{- $ct := include "vespa.container.fullname" . -}}
{{- $co := include "vespa.content.fullname" . -}}
<?xml version="1.0" encoding="utf-8" ?>
<hosts>
{{- range $i, $_ := until (int .Values.configserver.replicas) }}
  <host name="{{ $cs }}-{{ $i }}.{{ $cs }}.{{ $ns }}.svc.cluster.local">
    <alias>vespa-configserver-{{ $i }}</alias>
  </host>
{{- end }}
{{- range $i, $_ := until (int .Values.container.replicas) }}
  <host name="{{ $ct }}-{{ $i }}.{{ $ct }}.{{ $ns }}.svc.cluster.local">
    <alias>vespa-container-{{ $i }}</alias>
  </host>
{{- end }}
{{- range $i, $_ := until (int .Values.content.replicas) }}
  <host name="{{ $co }}-{{ $i }}.{{ $co }}.{{ $ns }}.svc.cluster.local">
    <alias>vespa-content-{{ $i }}</alias>
  </host>
{{- end }}
</hosts>
{{- end -}}
