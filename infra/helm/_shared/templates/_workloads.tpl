{{/*
ServiceAccount per service. One per Pod identity so workload-identity
annotations (EKS / GKE / AKS) can be scoped per service if needed.
Caller passes:
  .root      Helm root context
  .name      service name
  .release   release name
  .annotations optional map of extra annotations
*/}}
{{- define "of-shared.serviceaccount" -}}
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ .name }}
  labels:
    {{- include "of-shared.labels" . | nindent 4 }}
  {{- with .annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
  {{- end }}
automountServiceAccountToken: true
{{- end }}

{{/*
HorizontalPodAutoscaler v2. Caller passes:
  .name             service name
  .release          release name
  .autoscaling.minReplicas
  .autoscaling.maxReplicas
  .autoscaling.targetCPUUtilizationPercentage
  .autoscaling.targetMemoryUtilizationPercentage
*/}}
{{- define "of-shared.hpa" -}}
{{- $root := .root | default dict -}}
{{- $globalAutoscaling := (($root.Values.global | default dict).autoscaling | default dict) -}}
{{- $enabled := true -}}
{{- if hasKey $globalAutoscaling "enabled" -}}
  {{- $enabled = $globalAutoscaling.enabled -}}
{{- end -}}
{{- if $enabled }}
apiVersion: autoscaling/v2
kind: HorizontalPodAutoscaler
metadata:
  name: {{ .name }}
  labels:
    {{- include "of-shared.labels" . | nindent 4 }}
spec:
  scaleTargetRef:
    apiVersion: apps/v1
    kind: Deployment
    name: {{ .name }}
  minReplicas: {{ .autoscaling.minReplicas | default 2 }}
  maxReplicas: {{ .autoscaling.maxReplicas | default 6 }}
  metrics:
    - type: Resource
      resource:
        name: cpu
        target:
          type: Utilization
          averageUtilization: {{ .autoscaling.targetCPUUtilizationPercentage | default 70 }}
    - type: Resource
      resource:
        name: memory
        target:
          type: Utilization
          averageUtilization: {{ .autoscaling.targetMemoryUtilizationPercentage | default 80 }}
{{- end }}
{{- end }}

{{/*
KEDA ScaledObject. Caller passes:
  .root        Helm root context
  .name        service name
  .release     release name
  .autoscaling service autoscaling map
*/}}
{{- define "of-shared.scaledobject" -}}
{{- $root := .root | default dict -}}
{{- $globalAutoscaling := (($root.Values.global | default dict).autoscaling | default dict) -}}
{{- $enabled := true -}}
{{- if hasKey $globalAutoscaling "enabled" -}}
  {{- $enabled = $globalAutoscaling.enabled -}}
{{- end -}}
{{- if and $enabled .autoscaling .autoscaling.keda .autoscaling.keda.enabled }}
apiVersion: keda.sh/v1alpha1
kind: ScaledObject
metadata:
  name: {{ .name }}
  labels:
    {{- include "of-shared.labels" . | nindent 4 }}
spec:
  scaleTargetRef:
    name: {{ .name }}
  minReplicaCount: {{ .autoscaling.minReplicas | default 1 }}
  maxReplicaCount: {{ .autoscaling.maxReplicas | default 6 }}
  pollingInterval: {{ .autoscaling.keda.pollingInterval | default 15 }}
  cooldownPeriod: {{ .autoscaling.keda.cooldownPeriod | default 120 }}
  triggers:
    {{- toYaml (.autoscaling.keda.triggers | default (list)) | nindent 4 }}
{{- end }}
{{- end }}

{{/*
PodDisruptionBudget. Caller passes:
  .name           service name
  .release        release name
  .pdb.minAvailable   integer or "50%"
*/}}
{{- define "of-shared.pdb" -}}
apiVersion: policy/v1
kind: PodDisruptionBudget
metadata:
  name: {{ .name }}
  labels:
    {{- include "of-shared.labels" . | nindent 4 }}
spec:
  minAvailable: {{ .pdb.minAvailable | default 1 }}
  selector:
    matchLabels:
      app.kubernetes.io/name: {{ .name }}
{{- end }}

{{/*
Ingress for the release entrypoint. Caller passes:
  .name        ingress resource name (typically "<release>-gateway")
  .release     release name
  .ingress     map: { className, host, path, serviceName, servicePort, tls (optional), annotations (optional) }
Renders nothing if .ingress.enabled is false or unset.
*/}}
{{- define "of-shared.ingress" -}}
{{- if .ingress.enabled }}
{{- $root := .root | default dict -}}
{{- $global := $root.Values.global | default dict -}}
{{- $geo := (($global.deploymentFabric | default dict).geoRestrictions | default dict) -}}
{{- $annotations := mergeOverwrite (dict) (.ingress.annotations | default dict) ($geo.ingressAnnotations | default dict) -}}
{{- $allowedIngressCidrs := $geo.allowedIngressCidrs | default (list) -}}
{{- if and (gt (len $allowedIngressCidrs) 0) (not (hasKey $annotations "nginx.ingress.kubernetes.io/whitelist-source-range")) }}
{{- $_ := set $annotations "nginx.ingress.kubernetes.io/whitelist-source-range" (join "," $allowedIngressCidrs) -}}
{{- end }}
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ .name }}
  labels:
    {{- include "of-shared.labels" . | nindent 4 }}
  {{- if gt (len $annotations) 0 }}
  annotations:
    {{- toYaml $annotations | nindent 4 }}
  {{- end }}
spec:
  {{- with .ingress.className }}
  ingressClassName: {{ . }}
  {{- end }}
  {{- with .ingress.tls }}
  tls:
    {{- toYaml . | nindent 4 }}
  {{- end }}
  rules:
    {{- $hosts := .ingress.hosts | default (list) }}
    {{- if and (eq (len $hosts) 0) .ingress.host }}
      {{- $hosts = list (dict "host" .ingress.host "paths" (list (dict "path" (.ingress.path | default "/") "pathType" (.ingress.pathType | default "Prefix"))) ) }}
    {{- end }}
    {{- range $host := $hosts }}
    - host: {{ $host.host | quote }}
      http:
        paths:
          {{- range $path := ($host.paths | default (list (dict "path" ($.ingress.path | default "/") "pathType" ($.ingress.pathType | default "Prefix")))) }}
          - path: {{ $path.path | default "/" }}
            pathType: {{ $path.pathType | default "Prefix" }}
            backend:
              service:
                name: {{ $.ingress.serviceName }}
                port:
                  number: {{ $.ingress.servicePort }}
          {{- end }}
    {{- end }}
{{- end }}
{{- end }}
