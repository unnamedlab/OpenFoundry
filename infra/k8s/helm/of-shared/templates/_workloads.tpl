{{/*
ServiceAccount per service. One per Pod identity so workload-identity
annotations (EKS / GKE / AKS) can be scoped per service if needed.
Caller passes:
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
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ .name }}
  labels:
    {{- include "of-shared.labels" . | nindent 4 }}
  {{- with .ingress.annotations }}
  annotations:
    {{- toYaml . | nindent 4 }}
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
    - host: {{ .ingress.host | quote }}
      http:
        paths:
          - path: {{ .ingress.path | default "/" }}
            pathType: {{ .ingress.pathType | default "Prefix" }}
            backend:
              service:
                name: {{ .ingress.serviceName }}
                port:
                  number: {{ .ingress.servicePort }}
{{- end }}
{{- end }}
