{{/*
S8.3 — Postgres migrations as Helm `pre-upgrade,pre-install` Job.
Library charts cannot emit resources directly, so this is exposed as
a named template; each application chart instantiates it from its own
templates/ directory via `{{ include "of-shared.migrations" . }}`.

Activated per-database via `.Values.migrations.<db>.enabled: true`.
*/}}
{{- define "of-shared.migrations" -}}
{{- $root := . -}}
{{- $release := required "Values.release is required for migration resource names" $root.Values.release -}}
{{- $migratorName := printf "%s-migrator" $release | trunc 63 | trimSuffix "-" -}}
{{- $globalMigrations := (($root.Values.global | default dict).migrations | default dict) -}}
{{- $migrationsEnabled := true -}}
{{- if hasKey $globalMigrations "enabled" -}}
  {{- $migrationsEnabled = $globalMigrations.enabled -}}
{{- end -}}
{{- if $migrationsEnabled }}
{{- $hasEnabledMigration := false -}}
{{- range $_, $cfg := .Values.migrations }}
  {{- if $cfg.enabled }}
    {{- $hasEnabledMigration = true -}}
  {{- end }}
{{- end }}
{{- if $hasEnabledMigration }}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: {{ $migratorName }}
  labels:
    app.kubernetes.io/name: of-migrator
    app.kubernetes.io/instance: {{ $release }}
    app.kubernetes.io/managed-by: helm
    app.kubernetes.io/part-of: openfoundry
    openfoundry.io/release: {{ $release }}
automountServiceAccountToken: true
{{- end }}
{{- range $db, $cfg := .Values.migrations }}
{{- if $cfg.enabled }}
{{- $jobName := printf "%s-%s" $migratorName $db | trunc 63 | trimSuffix "-" -}}
{{ printf "\n---" }}
apiVersion: batch/v1
kind: Job
metadata:
  name: {{ $jobName }}
  annotations:
    "helm.sh/hook": pre-install,pre-upgrade
    "helm.sh/hook-weight": "-5"
    "helm.sh/hook-delete-policy": before-hook-creation,hook-succeeded
  labels:
    app.kubernetes.io/name: of-migrator
    app.kubernetes.io/instance: {{ $release }}
    app.kubernetes.io/managed-by: helm
    openfoundry.io/release: {{ $release }}
    openfoundry.io/migration-target: {{ $db }}
spec:
  backoffLimit: 1
  ttlSecondsAfterFinished: 600
  template:
    metadata:
      labels:
        app.kubernetes.io/name: of-migrator
        app.kubernetes.io/instance: {{ $release }}
        openfoundry.io/migration-target: {{ $db }}
    spec:
      restartPolicy: Never
      serviceAccountName: {{ $migratorName }}
      {{- with ($root.Values.global.imagePullSecrets | default (list)) }}
      imagePullSecrets:
        {{- range . }}
        - name: {{ . | quote }}
        {{- end }}
      {{- end }}
      containers:
        - name: migrator
          image: {{ $cfg.image | default (printf "%s/of-migrator:%s" $root.Values.image.registry $root.Values.image.tag) }}
          imagePullPolicy: IfNotPresent
          args:
            - migrate
            - run
            - --source
            - /migrations/{{ $db }}
          env:
            - name: DATABASE_URL
              valueFrom:
                secretKeyRef:
                  name: {{ $cfg.dsnSecret.name }}
                  key:  {{ $cfg.dsnSecret.key }}
          resources:
            requests: { cpu: "100m", memory: "128Mi" }
            limits:   { cpu: "500m", memory: "256Mi" }
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            runAsNonRoot: true
            runAsUser: 65532
            capabilities:
              drop: ["ALL"]
{{- end }}
{{- end }}
{{- end }}
{{- end }}
