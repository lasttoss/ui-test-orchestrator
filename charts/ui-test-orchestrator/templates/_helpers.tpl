{{- define "uito.name" -}}
{{- default .Chart.Name .Values.nameOverride | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "uito.fullname" -}}
{{- printf "%s-%s" .Release.Name (include "uito.name" .) | trunc 63 | trimSuffix "-" -}}
{{- end -}}

{{- define "uito.labels" -}}
app.kubernetes.io/name: {{ include "uito.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
app.kubernetes.io/managed-by: {{ .Release.Service }}
helm.sh/chart: {{ printf "%s-%s" .Chart.Name .Chart.Version | replace "+" "_" }}
{{- end -}}

{{- define "uito.image" -}}
{{- printf "%s:%s" .Values.image.repository (default .Chart.AppVersion .Values.image.tag) -}}
{{- end -}}

{{- define "uito.chromeImage" -}}
{{- printf "%s:%s" .Values.chrome.image .Values.chrome.tag -}}
{{- end -}}

{{- define "uito.scenarios" -}}
{{- if .Values.scenarios.existingConfigMap -}}
{{- .Values.scenarios.existingConfigMap -}}
{{- else -}}
{{- printf "%s-scenarios" (include "uito.fullname" .) -}}
{{- end -}}
{{- end -}}

{{- define "uito.serviceAccountName" -}}
{{- if .Values.serviceAccount.create -}}
{{- default (include "uito.fullname" .) .Values.serviceAccount.name -}}
{{- else -}}
{{- default "default" .Values.serviceAccount.name -}}
{{- end -}}
{{- end -}}

{{/*
The pod that runs a suite. It is a named template because it is used twice - by the Job and by the
CronJob - and two copies of a pod spec is how a CronJob ends up running a different suite than the Job
you tested.
*/}}
{{- define "uito.podSpec" -}}
restartPolicy: Never
serviceAccountName: {{ include "uito.serviceAccountName" . }}
securityContext:
  runAsNonRoot: true
  runAsUser: 10000
  seccompProfile:
    type: RuntimeDefault
containers:
  - name: suite
    image: {{ include "uito.image" . }}
    imagePullPolicy: {{ .Values.image.pullPolicy }}
    args:
      - run
      - --base-url={{ required "values.appURL is required: the suite has to know what it is testing" .Values.appURL }}
      - --scenarios={{ .Values.scenarios.dir }}
      - --parallel={{ .Values.parallel }}
      - --attempts={{ .Values.attempts }}
      - --state=/state/quarantine.json
      - --report=/report
      {{- if .Values.chrome.enabled }}
      - --cdp-url=http://127.0.0.1:{{ .Values.chrome.port }}
      {{- end }}
      {{- if .Values.cdpURL }}
      - --cdp-url={{ .Values.cdpURL }}
      {{- end }}
    resources: {{- toYaml .Values.resources | nindent 6 }}
    securityContext:
      allowPrivilegeEscalation: false
      capabilities:
        drop: ["ALL"]
    volumeMounts:
      - name: scenarios
        mountPath: {{ .Values.scenarios.dir }}
        readOnly: true
      - name: state
        mountPath: /state
      - name: report
        mountPath: /report
volumes:
  - name: scenarios
    configMap:
      name: {{ include "uito.scenarios" . }}
  - name: state
    {{- if .Values.quarantine.existingConfigMap }}
    configMap:
      name: {{ .Values.quarantine.existingConfigMap }}
    {{- else }}
    emptyDir: {}
    {{- end }}
  - name: report
    emptyDir: {}
{{- end -}}
