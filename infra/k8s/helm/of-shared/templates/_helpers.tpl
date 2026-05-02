{{/*
Common labels for every OpenFoundry resource. Consumed by the five
release-aligned charts (of-platform, of-data-engine, of-ontology,
of-ml-aip, of-apps-ops).
*/}}
{{- define "of-shared.labels" -}}
app.kubernetes.io/name: {{ .name }}
app.kubernetes.io/instance: {{ .release }}
app.kubernetes.io/managed-by: helm
app.kubernetes.io/part-of: openfoundry
openfoundry.io/release: {{ .release }}
{{- end }}

{{/*
Render a Deployment for one service. Caller passes:
  .name      service name (also Pod selector label)
  .release   Helm release name (of-platform | of-data-engine | ...)
  .image     full image reference
  .replicas  default replica count
  .env       list of {name,value} or {name,valueFrom} entries
  .ports     list of {name,containerPort}
  .resources resource requests/limits map
*/}}
{{- define "of-shared.deployment" -}}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .name }}
  labels:
    {{- include "of-shared.labels" . | nindent 4 }}
spec:
  replicas: {{ .replicas | default 2 }}
  selector:
    matchLabels:
      app.kubernetes.io/name: {{ .name }}
  template:
    metadata:
      labels:
        {{- include "of-shared.labels" . | nindent 8 }}
    spec:
      serviceAccountName: {{ .name }}
      containers:
        - name: app
          image: {{ .image }}
          imagePullPolicy: IfNotPresent
          ports:
            {{- range .ports }}
            - name: {{ .name }}
              containerPort: {{ .containerPort }}
            {{- end }}
          env:
            {{- toYaml .env | nindent 12 }}
          resources:
            {{- toYaml .resources | nindent 12 }}
          readinessProbe:
            httpGet: { path: /readyz, port: http }
            periodSeconds: 5
          livenessProbe:
            httpGet: { path: /healthz, port: http }
            periodSeconds: 10
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
            capabilities:
              drop: ["ALL"]
{{- end }}

{{/*
Render a ClusterIP Service for one service.
*/}}
{{- define "of-shared.service" -}}
apiVersion: v1
kind: Service
metadata:
  name: {{ .name }}
  labels:
    {{- include "of-shared.labels" . | nindent 4 }}
spec:
  type: ClusterIP
  selector:
    app.kubernetes.io/name: {{ .name }}
  ports:
    {{- range .ports }}
    - name: {{ .name }}
      port: {{ .containerPort }}
      targetPort: {{ .name }}
    {{- end }}
{{- end }}

{{/*
Render a default-deny + allow-from-platform NetworkPolicy.
Caller can override `.allowFrom` to a list of pod selectors.
*/}}
{{- define "of-shared.networkpolicy" -}}
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: {{ .name }}
  labels:
    {{- include "of-shared.labels" . | nindent 4 }}
spec:
  podSelector:
    matchLabels:
      app.kubernetes.io/name: {{ .name }}
  policyTypes: [Ingress, Egress]
  ingress:
    - from:
        - podSelector:
            matchLabels:
              openfoundry.io/release: of-platform
        {{- range .allowFrom }}
        - podSelector:
            matchLabels: {{ toYaml . | nindent 14 }}
        {{- end }}
  egress:
    - {} # default allow; tighten per release if needed
{{- end }}
