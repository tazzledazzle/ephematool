# Phase 2b · Create API: template catalog, preview access, environment telemetry, create endpoint

**Objective:** build the server half of "create a preview environment" without any cluster: the approved-template catalog with schema validation, the preview access gate (one-time ticket → environment-bound cookie, ForwardAuth), the collector pipelines that accept environment telemetry safely, the sample workload, and `POST /api/environments` with ordered validation, an audit row for every outcome and **zero dispatches**.

**Design references:** `.planning/phases/02-create-a-secure-preview-environment/` `02-CONTEXT.md` (D-05, D-06, D-07, D-10, D-12, D-13, D-19, D-20, D-22, D-23), `02-RESEARCH.md` (Patterns 3, 6, 10; "Create handler order"; Pitfalls 3, 4, 7, 8, 13), `02-PATTERNS.md` (sections 4, 6, 8, 11, 13, 14), `02-UI-SPEC.md` ("Schema-driven fields", "Server field errors").
**Plans covered:** 02-07 (templates + catalog), 02-08 (preview access), 02-09 (collector + sample app), 02-10 (create API).
**Requirements covered:** ENV-01, ENV-02, ENV-09, GRD-01, AUTH-02, AUTH-03, OBS-03 (server side).
**Prerequisite:** Phase 2a acceptance checklist complete (`phase-2a-foundations.md`, section 12). In particular `bazel test --config=integration //:test_integration` passes and `environments/conventions.gen.env` exists.
**Produces (later phases rely on these exact names):**

| Artifact | Used later for |
|---|---|
| `templates.FS`, `templates/sizes.yaml`, `templates/web-app/` | the runner (`provision.sh`, 2c) reads the same chart and presets the API validated against |
| `catalog.Load`, `ValidateValues`, `TrimForm`, `FormField` | create form (2d) renders `fields`; the API rejects bad values with field ids |
| `previewauth.NewForwardAuth`, `Tokens`, `SafeReturn`, `POST /api/preview/ticket` | Traefik ForwardAuth (2c), authorize page (2d) |
| `otlp/environments` receiver and `*/environments` pipelines | preview workloads' telemetry (2c kind, Phase 6 panels) |
| `cmd/sampleapp` | the workload kind verification deploys (2c) |
| `queue.TypeEnvDispatch/Poll/Probe/Sweep`, `QueueDispatch`, `EnvPayload`, `EnqueueEnvDispatch` | the worker (2c) |
| `envs.Service.Create`, `envs.NewID`, `envs.Rejection` | create endpoint now; reused by tests everywhere |
| `get-environment-options`, `create-environment`, `get-environment`, `list-environments` | dashboard (2d) |

> **How to read this document.** Every file is complete: copy it to the path shown. **replace** means take the whole file; **edit** shows the change. Commands are `bash` from the repository root. Where a file is marked "gazelle", run `bazel run //:gazelle` and let it write the `BUILD.bazel`.

---

## 1. What this phase adds

```
go.mod / MODULE.bazel                         # edit: jsonschema/v6, yaml.v3 and x/text become direct
templates/
├── embed.go, BUILD.bazel, sizes.yaml
└── web-app/{Chart.yaml,values.yaml,values.schema.json}
          └── templates/{_helpers.tpl,deployment.yaml,service.yaml,ingress.yaml,middleware.yaml}
internal/
├── catalog/{catalog.go,validate.go,form.go,sizes.go,catalog_test.go,form_test.go}
├── previewauth/{keys.go,tokens.go,safereturn.go,forwardauth.go,redisjti.go,ticket.go}
│              {helpers_test.go,tokens_test.go,safereturn_test.go,forwardauth_test.go,ticket_test.go}
├── envs/{ids.go,errors.go,telemetry.go,pgstore.go,service.go}
│       {service_test.go,create_integration_test.go}
├── queue/{tasks.go,client.go,client_test.go}              # replace / new test
└── httpapi/{api.go,environments.go,environments_test.go,environments_integration_test.go}
cmd/
├── api/main.go                                            # replace
└── sampleapp/{main.go,main_test.go,BUILD.bazel}
deploy/
├── otel/collector.yaml                                    # replace
├── compose.yaml                                           # edit: collector env_file + port 4320
└── compose.e2e.yaml                                       # replace
scripts/verify.d/env-telemetry.sh
BUILD.bazel                                                # edit: test_integration
api/openapi.json, web/src/api/schema.d.ts                  # regenerated
```

```bash
mkdir -p templates/web-app/templates internal/{catalog,previewauth,envs} cmd/sampleapp
```

---

## 2. Templates and size presets (plan 02-07, task 1)

An **approved template** is a Helm chart under `templates/`, versioned with the code and embedded in the API binary (D-10). Users never submit YAML; they pick a template and fill in the options its `values.schema.json` allows. Two kinds of values exist, and the schema keeps them apart:

- **User options** (`replicas`, `containerPort`, `healthPath`, `app.*`) are rendered in the form and validated server-side.
- **Platform values** (`image`, `platform.*`) are marked `"x-platform": true`. The API rejects any attempt to set them, and the runner passes them with `--set` after `-f`, so they always win.

Three guardrails are **not** in the chart on purpose, because a chart author could omit them: authentication (a Traefik *entrypoint* middleware, D-20), resource quota and LimitRange (the namespace baseline from `sizes.yaml`, 2c), and network policy (2c).

### 2.1 `templates/sizes.yaml`

The only way a user chooses CPU and memory (GRD-01). It is read twice: by the API for the picker and by the runner when it builds the namespace quota, so what is shown is what is enforced.

`templates/sizes.yaml`

```yaml
# Size presets: the ONLY way a user chooses CPU and memory (GRD-01, D-12).
# This file is read twice: by the API (internal/catalog) for the picker and
# validation, and by provision.sh as a Helm/quota values source, so the numbers
# the user sees are the numbers the namespace enforces.
sizeOrder: [S, M, L]
sizes:
  S:
    limits: {cpu: 500m, memory: 512Mi}               # namespace quota (hard ceiling)
    requests: {cpu: 250m, memory: 256Mi}
    containerDefault: {cpu: 100m, memory: 128Mi}     # LimitRange default limit
    containerDefaultRequest: {cpu: 50m, memory: 64Mi}
    pods: 10
    services: 5
    display: {cpu: "0.5 vCPU", memory: "512 MiB", helper: "Small: demos and smoke checks"}
  M:
    limits: {cpu: "1", memory: 1Gi}
    requests: {cpu: 500m, memory: 512Mi}
    containerDefault: {cpu: 200m, memory: 256Mi}
    containerDefaultRequest: {cpu: 100m, memory: 128Mi}
    pods: 15
    services: 5
    display: {cpu: "1 vCPU", memory: "1 GiB", helper: "Medium: typical web app and database"}
  L:
    limits: {cpu: "2", memory: 2Gi}
    requests: {cpu: "1", memory: 1Gi}
    containerDefault: {cpu: 250m, memory: 256Mi}
    containerDefaultRequest: {cpu: 125m, memory: 128Mi}
    pods: 20
    services: 8
    display: {cpu: "2 vCPU", memory: "2 GiB", helper: "Large: heavier services; uses more of the shared cluster"}
```
### 2.2 The `web-app` chart

`Chart.yaml` carries the version the API records as `chart_version`; `provision.sh` fails fast if the checked-out chart differs from it (chart/server skew).

`templates/web-app/Chart.yaml`

```yaml
apiVersion: v2
name: web-app
description: A single web service with a health endpoint, exposed through the preview gateway.
type: application
# provision.sh passes this as chart_version and fails fast if the checked-out chart differs
# from what the API validated against (chart/server skew).
version: 0.1.0
appVersion: "1"
```
`values.yaml` holds the defaults. `image` has **no** default so a missing image fails the render instead of running something unpinned.

`templates/web-app/values.yaml`

```yaml
# Defaults the user may override (the schema allows only these keys plus the platform block).
replicas: 1
containerPort: 8080
healthPath: /health
app:
  logLevel: info
  debug: false

# image and platform.* are set by provision.sh with --set AFTER -f, so they always win
# over anything in a values file. Users cannot set them (values.schema.json: x-platform).
# image has no default on purpose: a missing image must fail the render, not run "latest".
platform:
  envId: ""
  host: ""
  otelEndpoint: "http://host.docker.internal:4320"
  otelResourceAttributes: ""
  serviceName: preview-app
```
`values.schema.json` (draft-07) is enforced by Helm at install time **and** by the Go server before anything is created. `additionalProperties: false` at every level means a typo is an error, not a silently ignored value.

`templates/web-app/values.schema.json`

```json
{
  "$schema": "http://json-schema.org/draft-07/schema#",
  "title": "web-app values",
  "type": "object",
  "additionalProperties": false,
  "properties": {
    "replicas": {
      "type": "integer",
      "title": "Replicas",
      "description": "How many copies of the app to run.",
      "minimum": 1,
      "maximum": 3,
      "default": 1
    },
    "containerPort": {
      "type": "integer",
      "title": "Container port",
      "description": "The port your app listens on.",
      "minimum": 1024,
      "maximum": 65535,
      "default": 8080
    },
    "healthPath": {
      "type": "string",
      "title": "Health path",
      "description": "HTTP path that returns 2xx when the app is ready.",
      "pattern": "^/[A-Za-z0-9/_.-]{0,100}$",
      "default": "/health"
    },
    "app": {
      "type": "object",
      "title": "App settings",
      "additionalProperties": false,
      "properties": {
        "logLevel": {
          "type": "string",
          "title": "Log level",
          "enum": ["debug", "info", "warn"],
          "x-labels": ["Debug", "Info", "Warning"],
          "default": "info"
        },
        "debug": {
          "type": "boolean",
          "title": "Debug mode",
          "default": false
        }
      }
    },
    "image": {
      "type": "string",
      "x-platform": true,
      "pattern": "^ghcr\\.io/[a-z0-9._/-]+(@sha256:[0-9a-f]{64}|:[A-Za-z0-9_.-]{1,128})$"
    },
    "platform": {
      "type": "object",
      "x-platform": true,
      "additionalProperties": false,
      "properties": {
        "envId": {"type": "string"},
        "host": {"type": "string"},
        "otelEndpoint": {"type": "string"},
        "otelResourceAttributes": {"type": "string"},
        "serviceName": {"type": "string"}
      }
    }
  }
}
```
The templates. Note what the pod gets: no service-account token, non-root, read-only root filesystem, all capabilities dropped, seccomp `RuntimeDefault`; and what it deliberately does **not** get: a `resources:` block (the namespace LimitRange supplies defaults, the quota enforces the ceiling). The chart contains no `ephematool.io/` or `ephematool.env.` literal; the conventions gate scans `templates/` (section 6 of 2a), and `provision.sh` builds the label and attribute strings from `conventions.gen.env`.

`templates/web-app/templates/_helpers.tpl`

```text
{{- /* Selector labels. No platform label keys appear here: the namespace carries them
       (set by provision.sh from conventions.gen.env), and the conventions gate scans templates/. */ -}}
{{- define "web-app.name" -}}{{ .Chart.Name }}{{- end -}}
{{- define "web-app.selector" -}}
app.kubernetes.io/name: {{ include "web-app.name" . }}
app.kubernetes.io/instance: {{ .Release.Name }}
{{- end -}}
```
`templates/web-app/templates/deployment.yaml`

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ include "web-app.name" . }}
  labels:
    {{- include "web-app.selector" . | nindent 4 }}
spec:
  replicas: {{ .Values.replicas }}
  selector:
    matchLabels:
      {{- include "web-app.selector" . | nindent 6 }}
  template:
    metadata:
      labels:
        {{- include "web-app.selector" . | nindent 8 }}
    spec:
      # The app never talks to the Kubernetes API, so it gets no token (T-02-07-03).
      automountServiceAccountToken: false
      securityContext:
        runAsNonRoot: true
        seccompProfile:
          type: RuntimeDefault
      containers:
        - name: app
          image: {{ required "image is set by the platform (--set image=...)" .Values.image | quote }}
          imagePullPolicy: IfNotPresent
          # No resources block on purpose: the namespace LimitRange supplies the preset's
          # defaults and the ResourceQuota enforces the ceiling.
          securityContext:
            allowPrivilegeEscalation: false
            readOnlyRootFilesystem: true
            capabilities:
              drop: ["ALL"]
          ports:
            - name: http
              containerPort: {{ .Values.containerPort }}
          readinessProbe:
            httpGet:
              path: {{ .Values.healthPath }}
              port: http
            periodSeconds: 3
          env:
            - name: PORT
              value: {{ .Values.containerPort | quote }}
            - name: LOG_LEVEL
              value: {{ .Values.app.logLevel | quote }}
            - name: DEBUG
              value: {{ .Values.app.debug | quote }}
            # Telemetry (OBS-03): the app exports OTLP to the environment collector pipeline.
            # The resource attributes (env id, owner, template) are built by provision.sh.
            - name: OTEL_EXPORTER_OTLP_ENDPOINT
              value: {{ .Values.platform.otelEndpoint | quote }}
            - name: OTEL_EXPORTER_OTLP_PROTOCOL
              value: http/protobuf
            - name: OTEL_SERVICE_NAME
              value: {{ .Values.platform.serviceName | quote }}
            - name: OTEL_RESOURCE_ATTRIBUTES
              value: {{ .Values.platform.otelResourceAttributes | quote }}
          volumeMounts:
            - name: tmp
              mountPath: /tmp
      volumes:
        - name: tmp
          emptyDir: {}
```
`templates/web-app/templates/service.yaml`

```yaml
apiVersion: v1
kind: Service
metadata:
  name: {{ include "web-app.name" . }}
  labels:
    {{- include "web-app.selector" . | nindent 4 }}
spec:
  type: ClusterIP
  selector:
    {{- include "web-app.selector" . | nindent 4 }}
  ports:
    - name: http
      port: 80
      targetPort: http
```
Two Ingress objects: an **exact** route for the platform health path, rewritten by a Traefik Middleware to the app's own health path (so the prober never needs a credential, D-05), and a `Prefix /` route for everything else. Authentication is attached at the Traefik entrypoint, not here.

`templates/web-app/templates/ingress.yaml`

```yaml
{{- /* AUTH IS NOT IN THIS CHART. Access control is a Traefik ENTRYPOINT middleware (D-20), so a
       template author cannot forget or remove it. This chart only declares routes. */ -}}
{{- $host := required "platform.host is set by the platform (--set platform.host=...)" .Values.platform.host }}
# Exact route for the platform health probe; it is rewritten to the app's health path.
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "web-app.name" . }}-health
  annotations:
    traefik.ingress.kubernetes.io/router.middlewares: {{ .Release.Namespace }}-health-rewrite@kubernetescrd
spec:
  ingressClassName: traefik
  rules:
    - host: {{ $host | quote }}
      http:
        paths:
          - path: /__ephematool/healthz
            pathType: Exact
            backend:
              service:
                name: {{ include "web-app.name" . }}
                port: {name: http}
---
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{ include "web-app.name" . }}
spec:
  ingressClassName: traefik
  rules:
    - host: {{ $host | quote }}
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: {{ include "web-app.name" . }}
                port: {name: http}
```
`templates/web-app/templates/middleware.yaml`

```yaml
# Rewrites the exempt platform health path to the app's own health path (D-05).
apiVersion: traefik.io/v1alpha1
kind: Middleware
metadata:
  name: health-rewrite
spec:
  replacePath:
    path: {{ .Values.healthPath | quote }}
```
### 2.3 `templates/embed.go` and `templates/BUILD.bazel`

`all:web-app` matters: without `all:`, Go's embed silently skips files starting with `_` or `.`, and `_helpers.tpl` would be missing from the binary. `embedsrcs` in Bazel is an explicit list; add every new chart file by hand.

`templates/embed.go`

```go
// Package templates embeds the approved preview-environment charts and the
// shared size presets so the API binary carries exactly what the runner deploys.
package templates

import "embed"

// FS holds sizes.yaml plus every file of every chart. The "all:" prefix keeps
// files that start with "_" or "." (for example _helpers.tpl), which a plain
// directory embed would silently skip.
//
//go:embed sizes.yaml all:web-app
var FS embed.FS
```
`templates/BUILD.bazel`

```python
load("@io_bazel_rules_go//go:def.bzl", "go_library")

# embedsrcs is explicit: gazelle does not manage it. Add every new chart file here.
go_library(
    name = "templates",
    srcs = ["embed.go"],
    embedsrcs = [
        "sizes.yaml",
        "web-app/Chart.yaml",
        "web-app/values.yaml",
        "web-app/values.schema.json",
        "web-app/templates/_helpers.tpl",
        "web-app/templates/deployment.yaml",
        "web-app/templates/ingress.yaml",
        "web-app/templates/middleware.yaml",
        "web-app/templates/service.yaml",
    ],
    importpath = "github.com/ephematool/ephematool/templates",
    visibility = ["//visibility:public"],
)

# The runner (provision.sh) and the tests read the chart directory directly.
filegroup(
    name = "charts",
    srcs = glob(["**/*"], exclude = ["BUILD.bazel", "embed.go"]),
    visibility = ["//visibility:public"],
)
```
### 2.4 Check the chart yourself (needs `helm`, installed under `.tools/` in 2c, or any Helm 3.14+/4)

```bash
IMG="ghcr.io/ephematool-e2e/demo-app@sha256:$(printf 'a%.0s' {1..64})"
HOST="pv-aaaaaaaaaa.preview.localhost"          # no port: a Kubernetes Ingress host is a bare DNS name
helm lint templates/web-app --set image="$IMG" --set platform.host="$HOST"                 # 0 failures
helm template t templates/web-app --set image="$IMG" --set platform.host="$HOST" | head -40
helm template t templates/web-app --set image="$IMG" --set platform.host="$HOST" --set replicas=9   # must FAIL: maximum
helm template t templates/web-app                                                           # must FAIL: image is set by the platform
grep -rn 'ephematool\.io/\|ephematool\.env\.' templates/ && echo "BAD: hand-written key" || echo "no hand-written keys"
```

```bash
bazel run //:gazelle
bazel build //templates && bash scripts/verify.d/conventions.sh
```

---

## 3. The catalog (plan 02-07, task 2)

The catalog loads every chart from `templates.FS`, compiles its schema once, trims it into a form description for the UI, and reads the size presets. It answers three questions: *what can be created* (`Templates`, `Sizes`), *what does the form look like* (`TrimForm`), and *are these values acceptable* (`ValidateValues`).

```bash
go get github.com/santhosh-tekuri/jsonschema/v6@v6.0.3     # approved in 2a section 2; the engine Helm uses
go get gopkg.in/yaml.v3 golang.org/x/text                   # already indirect; now imported directly
go mod tidy && bazel mod tidy
```

Design points worth knowing before reading the code:

- **Field errors, not a blob.** A violation at JSON pointer `/replicas` becomes the UI field id `values.replicas`. A violation with no field (for example an unknown key at the root) has field `values` and its pointer in `path`, and the UI shows it in the summary only.
- **All errors, always.** `ValidateValues` returns every violation so the user fixes the form in one pass.
- **Platform keys first.** A user key marked `x-platform` is rejected as "set by the platform" before the schema runs, so the message explains why instead of a confusing pattern failure.
- **A nil message printer panics** in jsonschema/v6 (`ErrorKind.LocalizedString`). The code always passes `message.NewPrinter(language.English)`.
- **Values are normalised through JSON** before validation, because the validator wants `json.Number`/`float64`, not the Go `int` a caller may have built.
- **The form is a pure function of the schema** (`TrimForm`): supported constructs render, everything else is omitted (the chart default applies), and a *required* property that cannot be rendered and has no default makes the whole template unusable.

### 3.1 `internal/catalog/form.go`

This file is stdlib-only. Property order is preserved by reading the schema's `properties` with a token decoder, because the UI renders in schema order and Go maps do not keep it.

`internal/catalog/form.go`

```go
package catalog

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// FormField is one renderable control derived from a chart's values.schema.json.
// Path is the dotted id the UI uses and the API reports errors against
// ("values.app.logLevel"); Key is the last segment.
type FormField struct {
	Path        string      `json:"path"`
	Key         string      `json:"key"`
	Type        string      `json:"type" enum:"string,integer,number,boolean,enum,object"`
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Default     any         `json:"default,omitempty"`
	Required    bool        `json:"required"`
	Minimum     *float64    `json:"minimum,omitempty"`
	Maximum     *float64    `json:"maximum,omitempty"`
	MinLength   *int        `json:"minLength,omitempty"`
	MaxLength   *int        `json:"maxLength,omitempty"`
	Enum        []string    `json:"enum,omitempty"`
	Labels      []string    `json:"labels,omitempty"`
	Children    []FormField `json:"children,omitempty"`
}

// FormSchema is what the UI renders. Usable=false means the form cannot produce a
// valid request (a required property is not renderable and has no default), so the
// picker disables the template. OmittedSome=true shows the "advanced options" note.
type FormSchema struct {
	Usable      bool        `json:"usable"`
	OmittedSome bool        `json:"omittedSome"`
	Fields      []FormField `json:"fields"`
}

// unsupportedKeywords make a property non-renderable (UI-SPEC "Schema-driven fields").
var unsupportedKeywords = []string{"oneOf", "anyOf", "allOf", "not", "$ref", "if", "then", "else", "patternProperties"}

// TrimForm reduces a JSON Schema to the constructs the form supports: string, string enum,
// integer/number with bounds, boolean and one-level objects. Everything else is omitted
// (the chart default applies). Properties marked x-platform are skipped silently: they are
// not user options, so omitting them does not count as "omitted some".
// It is a pure function so it can be tested with a table.
func TrimForm(raw []byte) (FormSchema, error) {
	root, err := parseNode(raw)
	if err != nil {
		return FormSchema{}, fmt.Errorf("parse schema: %w", err)
	}
	t := &trimmer{usable: true}
	fields := t.fields(root, "values", 0)
	return FormSchema{Usable: t.usable, OmittedSome: t.omitted, Fields: fields}, nil
}

// node is a JSON Schema object that remembers the ORDER of its properties; Go maps do not,
// and the UI renders in schema order.
type node struct {
	m     map[string]json.RawMessage
	order []string // property names in document order
}

func parseNode(raw []byte) (*node, error) {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	n := &node{m: m}
	if p, ok := m["properties"]; ok {
		keys, err := orderedKeys(p)
		if err != nil {
			return nil, err
		}
		n.order = keys
	}
	return n, nil
}

// orderedKeys returns the top-level keys of a JSON object in document order.
func orderedKeys(raw json.RawMessage) ([]string, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if tok, err := dec.Token(); err != nil || tok != json.Delim('{') {
		return nil, fmt.Errorf("properties is not an object")
	}
	var keys []string
	for dec.More() {
		k, err := dec.Token()
		if err != nil {
			return nil, err
		}
		keys = append(keys, k.(string))
		var skip json.RawMessage // consume the value so the next token is a key
		if err := dec.Decode(&skip); err != nil {
			return nil, err
		}
	}
	return keys, nil
}

func (n *node) str(key string) string {
	var s string
	_ = json.Unmarshal(n.m[key], &s) // absent or wrong type -> ""
	return s
}

func (n *node) has(key string) bool { _, ok := n.m[key]; return ok }

func (n *node) bool(key string) bool {
	var b bool
	_ = json.Unmarshal(n.m[key], &b)
	return b
}

func (n *node) number(key string) *float64 {
	if !n.has(key) {
		return nil
	}
	var f float64
	if json.Unmarshal(n.m[key], &f) != nil {
		return nil
	}
	return &f
}

func (n *node) integer(key string) *int {
	f := n.number(key)
	if f == nil {
		return nil
	}
	i := int(*f)
	return &i
}

func (n *node) required() map[string]bool {
	var names []string
	_ = json.Unmarshal(n.m["required"], &names)
	out := make(map[string]bool, len(names))
	for _, s := range names {
		out[s] = true
	}
	return out
}

func (n *node) property(name string) (*node, error) {
	var props map[string]json.RawMessage
	if err := json.Unmarshal(n.m["properties"], &props); err != nil {
		return nil, err
	}
	return parseNode(props[name])
}

type trimmer struct {
	usable  bool
	omitted bool
}

// fields renders the properties of obj. depth 0 is the schema root; depth 1 is inside a
// top-level object. Objects below depth 1 are not rendered.
func (t *trimmer) fields(obj *node, prefix string, depth int) []FormField {
	required := obj.required()
	var out []FormField
	for _, key := range obj.order {
		prop, err := obj.property(key)
		if err != nil {
			t.omit(required[key], false)
			continue
		}
		if prop.bool("x-platform") {
			continue // platform-owned: never a user option, never counted as omitted
		}
		f, ok := t.field(prop, key, prefix+"."+key, required[key], depth)
		if !ok {
			t.omit(required[key], prop.has("default"))
			continue
		}
		out = append(out, f)
	}
	return out
}

// omit records a non-renderable property. A required property with no default means the
// form can never build a valid request, so the template becomes unusable.
func (t *trimmer) omit(required, hasDefault bool) {
	t.omitted = true
	if required && !hasDefault {
		t.usable = false
	}
}

func (t *trimmer) field(p *node, key, path string, required bool, depth int) (FormField, bool) {
	for _, k := range unsupportedKeywords {
		if p.has(k) {
			return FormField{}, false
		}
	}
	f := FormField{
		Path: path, Key: key, Title: titleOf(p, key), Description: p.str("description"),
		Required: required,
	}
	if p.has("default") {
		var d any
		if json.Unmarshal(p.m["default"], &d) == nil {
			f.Default = d
		}
	}
	typ := p.str("type")
	switch {
	case p.has("enum"):
		var vals []string
		if json.Unmarshal(p.m["enum"], &vals) != nil || len(vals) == 0 {
			return FormField{}, false // non-string enums are not supported
		}
		f.Type, f.Enum = "enum", vals
		var labels []string
		if json.Unmarshal(p.m["x-labels"], &labels) == nil && len(labels) == len(vals) {
			f.Labels = labels
		}
	case typ == "string":
		f.Type, f.MinLength, f.MaxLength = "string", p.integer("minLength"), p.integer("maxLength")
	case typ == "integer" || typ == "number":
		f.Type, f.Minimum, f.Maximum = typ, p.number("minimum"), p.number("maximum")
	case typ == "boolean":
		f.Type = "boolean"
	case typ == "object":
		if depth >= 1 {
			return FormField{}, false // objects nested deeper than one level
		}
		f.Type = "object"
		f.Children = t.fields(p, path, depth+1)
		if len(f.Children) == 0 {
			return FormField{}, false
		}
	default:
		return FormField{}, false // array, null, no type (const-only) and unknown types
	}
	return f, true
}

// titleOf prefers the schema title, else a humanized key ("logLevel" -> "Log level").
func titleOf(p *node, key string) string {
	if t := p.str("title"); t != "" {
		return t
	}
	var b strings.Builder
	for i, r := range key {
		if i > 0 && r >= 'A' && r <= 'Z' {
			b.WriteByte(' ')
			b.WriteRune(r + 32)
			continue
		}
		if i == 0 && r >= 'a' && r <= 'z' {
			r -= 32
		}
		b.WriteRune(r)
	}
	return b.String()
}
```
### 3.2 `internal/catalog/sizes.go`

`internal/catalog/sizes.go`

```go
package catalog

import (
	"fmt"
	"io/fs"

	"gopkg.in/yaml.v3"
)

// Size is one preset as the API and UI present it. The numbers are the namespace quota
// ceiling; provision.sh reads the same sizes.yaml, so what is shown is what is enforced.
type Size struct {
	Name           string `json:"name"`
	LimitsCPU      string `json:"limitsCpu"`
	LimitsMemory   string `json:"limitsMemory"`
	RequestsCPU    string `json:"requestsCpu"`
	RequestsMemory string `json:"requestsMemory"`
	DisplayCPU     string `json:"displayCpu"`
	DisplayMemory  string `json:"displayMemory"`
	Helper         string `json:"helper"`
}

// sizesFile mirrors templates/sizes.yaml (only the fields the API needs).
type sizesFile struct {
	SizeOrder []string `yaml:"sizeOrder"`
	Sizes     map[string]struct {
		Limits   struct{ CPU, Memory string }         `yaml:"limits"`
		Requests struct{ CPU, Memory string }         `yaml:"requests"`
		Display  struct{ CPU, Memory, Helper string } `yaml:"display"`
	} `yaml:"sizes"`
}

func loadSizes(fsys fs.FS) ([]Size, error) {
	raw, err := fs.ReadFile(fsys, "sizes.yaml")
	if err != nil {
		return nil, fmt.Errorf("read sizes.yaml: %w", err)
	}
	var f sizesFile
	if err := yaml.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("parse sizes.yaml: %w", err)
	}
	if len(f.SizeOrder) == 0 {
		return nil, fmt.Errorf("sizes.yaml: sizeOrder is empty")
	}
	out := make([]Size, 0, len(f.SizeOrder))
	for _, name := range f.SizeOrder {
		s, ok := f.Sizes[name]
		if !ok {
			return nil, fmt.Errorf("sizes.yaml: sizeOrder names %q but sizes has no such entry", name)
		}
		out = append(out, Size{
			Name: name, LimitsCPU: s.Limits.CPU, LimitsMemory: s.Limits.Memory,
			RequestsCPU: s.Requests.CPU, RequestsMemory: s.Requests.Memory,
			DisplayCPU: s.Display.CPU, DisplayMemory: s.Display.Memory, Helper: s.Display.Helper,
		})
	}
	return out, nil
}
```
### 3.3 `internal/catalog/catalog.go`

`internal/catalog/catalog.go`

```go
// Package catalog is the approved-template catalog (D-10): Helm charts embedded in the
// binary, each with a JSON Schema for its values, plus the shared S/M/L size presets.
package catalog

import (
	"bytes"
	"fmt"
	"io/fs"
	"path"
	"sort"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"gopkg.in/yaml.v3"
)

// Template is one approved chart as the API presents it.
type Template struct {
	Name        string     `json:"name"`
	Version     string     `json:"version"`
	Description string     `json:"description"`
	Form        FormSchema `json:"form"`
}

type entry struct {
	tpl      Template
	schema   *jsonschema.Schema
	platform map[string]bool // top-level value keys the user may not set (x-platform)
}

// Catalog is immutable after Load and safe for concurrent use.
type Catalog struct {
	byName map[string]entry
	names  []string // sorted, so listings are stable
	sizes  []Size
}

// Load reads every top-level directory of fsys that contains a Chart.yaml, compiles its
// values.schema.json once, trims it for the form, and reads sizes.yaml.
// Pass templates.FS in production and an fstest.MapFS in tests.
func Load(fsys fs.FS) (*Catalog, error) {
	sizes, err := loadSizes(fsys)
	if err != nil {
		return nil, err
	}
	dirs, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, err
	}
	c := &Catalog{byName: map[string]entry{}, sizes: sizes}
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		e, err := loadChart(fsys, d.Name())
		if err != nil {
			return nil, fmt.Errorf("template %s: %w", d.Name(), err)
		}
		c.byName[e.tpl.Name] = e
		c.names = append(c.names, e.tpl.Name)
	}
	sort.Strings(c.names)
	if len(c.names) == 0 {
		return nil, fmt.Errorf("no templates found (a directory with Chart.yaml)")
	}
	return c, nil
}

func loadChart(fsys fs.FS, dir string) (entry, error) {
	chartRaw, err := fs.ReadFile(fsys, path.Join(dir, "Chart.yaml"))
	if err != nil {
		return entry{}, err
	}
	var chart struct {
		Name        string `yaml:"name"`
		Version     string `yaml:"version"`
		Description string `yaml:"description"`
	}
	if err := yaml.Unmarshal(chartRaw, &chart); err != nil {
		return entry{}, fmt.Errorf("Chart.yaml: %w", err)
	}
	if chart.Name != dir {
		return entry{}, fmt.Errorf("Chart.yaml name %q must equal its directory %q", chart.Name, dir)
	}
	raw, err := fs.ReadFile(fsys, path.Join(dir, "values.schema.json"))
	if err != nil {
		return entry{}, err
	}

	// Compile once. UnmarshalJSON keeps numbers as json.Number, which the validator expects.
	doc, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return entry{}, fmt.Errorf("values.schema.json: %w", err)
	}
	comp := jsonschema.NewCompiler()
	const url = "mem://schema.json"
	if err := comp.AddResource(url, doc); err != nil {
		return entry{}, err
	}
	schema, err := comp.Compile(url)
	if err != nil {
		return entry{}, fmt.Errorf("compile schema: %w", err)
	}
	form, err := TrimForm(raw)
	if err != nil {
		return entry{}, err
	}
	platform, err := platformKeys(raw)
	if err != nil {
		return entry{}, err
	}
	return entry{
		tpl:      Template{Name: chart.Name, Version: chart.Version, Description: chart.Description, Form: form},
		schema:   schema,
		platform: platform,
	}, nil
}

// Templates lists approved templates in name order.
func (c *Catalog) Templates() []Template {
	out := make([]Template, 0, len(c.names))
	for _, n := range c.names {
		out = append(out, c.byName[n].tpl)
	}
	return out
}

// Template returns one template by name.
func (c *Catalog) Template(name string) (Template, bool) {
	e, ok := c.byName[name]
	return e.tpl, ok
}

// Sizes lists presets in sizeOrder (S, M, L).
func (c *Catalog) Sizes() []Size { return append([]Size(nil), c.sizes...) }

// Size returns one preset by name.
func (c *Catalog) Size(name string) (Size, bool) {
	for _, s := range c.sizes {
		if s.Name == name {
			return s, true
		}
	}
	return Size{}, false
}
```
### 3.4 `internal/catalog/validate.go`

`internal/catalog/validate.go`

```go
package catalog

import (
	"encoding/json"
	"errors"
	"sort"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// FieldError is one problem with the submitted values. Field is the UI field id
// ("values.replicas"); a problem with no matching field has Field "values" and Path set to
// the JSON pointer, which the UI shows in the summary only.
type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

const codeSchemaViolation = "schema_violation"

// platformKeys returns the top-level properties marked "x-platform": true.
func platformKeys(raw []byte) (map[string]bool, error) {
	var s struct {
		Properties map[string]struct {
			Platform bool `json:"x-platform"`
		} `json:"properties"`
	}
	if err := json.Unmarshal(raw, &s); err != nil {
		return nil, err
	}
	out := map[string]bool{}
	for k, p := range s.Properties {
		if p.Platform {
			out[k] = true
		}
	}
	return out, nil
}

// ValidateValues checks user-supplied template values and returns EVERY violation (never
// just the first), sorted by field so responses are deterministic. An unknown template
// yields a single error on "template".
func (c *Catalog) ValidateValues(template string, values map[string]any) []FieldError {
	e, ok := c.byName[template]
	if !ok {
		return []FieldError{{Field: "template", Code: "unknown_template", Message: "Select an approved template."}}
	}
	var out []FieldError

	// 1. Platform-owned keys are never user input (T-02-07-01). Reject them before the schema
	//    runs so the message says why, instead of a confusing pattern failure.
	for k := range values {
		if e.platform[k] {
			out = append(out, FieldError{Field: "values." + k, Code: codeSchemaViolation, Message: "This value is set by the platform."})
		}
	}

	// 2. Normalize through JSON: the validator wants json.Number/float64/map[string]any, not
	//    Go ints or typed maps a caller may have built.
	raw, err := json.Marshal(values)
	if err != nil {
		return append(out, FieldError{Field: "values", Code: codeSchemaViolation, Message: "Values could not be read.", Path: "/"})
	}
	inst, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		return append(out, FieldError{Field: "values", Code: codeSchemaViolation, Message: "Values could not be read.", Path: "/"})
	}

	if err := e.schema.Validate(inst); err != nil {
		var ve *jsonschema.ValidationError
		if errors.As(err, &ve) {
			// A nil printer panics inside LocalizedString; always pass a real one.
			p := message.NewPrinter(language.English)
			walk(ve, p, e.platform, &out)
		} else {
			out = append(out, FieldError{Field: "values", Code: codeSchemaViolation, Message: err.Error(), Path: "/"})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Field < out[j].Field })
	return out
}

// walk collects the leaf causes of a validation error tree.
func walk(ve *jsonschema.ValidationError, p *message.Printer, platform map[string]bool, out *[]FieldError) {
	if len(ve.Causes) > 0 {
		for _, c := range ve.Causes {
			walk(c, p, platform, out)
		}
		return
	}
	loc := ve.InstanceLocation
	if len(loc) > 0 && platform[loc[0]] {
		return // already reported as "set by the platform"; do not double-report
	}
	fe := FieldError{Code: codeSchemaViolation, Message: ve.ErrorKind.LocalizedString(p)}
	if len(loc) == 0 {
		fe.Field, fe.Path = "values", "/" // no field to attach to: summary only
	} else {
		fe.Field = "values." + strings.Join(loc, ".")
	}
	*out = append(*out, fe)
}
```
### 3.5 Tests

`internal/catalog/form_test.go`

```go
package catalog

import (
	"os"
	"testing"
)

func TestTrimFormWebApp(t *testing.T) {
	raw, err := os.ReadFile("../../templates/web-app/values.schema.json")
	if err != nil {
		t.Skip("run inside the repository: " + err.Error())
	}
	fs, err := TrimForm(raw)
	if err != nil {
		t.Fatal(err)
	}
	if !fs.Usable || fs.OmittedSome {
		t.Fatalf("usable=%v omitted=%v, want true/false (x-platform props are not 'omitted')", fs.Usable, fs.OmittedSome)
	}
	var keys []string
	for _, f := range fs.Fields {
		keys = append(keys, f.Key)
	}
	want := []string{"replicas", "containerPort", "healthPath", "app"}
	if len(keys) != len(want) {
		t.Fatalf("keys=%v want %v (schema order, no image/platform)", keys, want)
	}
	for i := range want {
		if keys[i] != want[i] {
			t.Fatalf("keys=%v want %v", keys, want)
		}
	}
	rep := fs.Fields[0]
	if rep.Type != "integer" || rep.Minimum == nil || *rep.Minimum != 1 || rep.Maximum == nil || *rep.Maximum != 3 {
		t.Fatalf("replicas wrong: %+v", rep)
	}
	app := fs.Fields[3]
	if app.Type != "object" || len(app.Children) != 2 {
		t.Fatalf("app wrong: %+v", app)
	}
	lvl := app.Children[0]
	if lvl.Path != "values.app.logLevel" || lvl.Type != "enum" || len(lvl.Labels) != 3 || lvl.Labels[2] != "Warning" {
		t.Fatalf("logLevel wrong: %+v", lvl)
	}
	if app.Children[1].Type != "boolean" {
		t.Fatalf("debug wrong: %+v", app.Children[1])
	}
}

func TestTrimFormUnsupported(t *testing.T) {
	tests := []struct {
		name        string
		schema      string
		wantUsable  bool
		wantOmitted bool
		wantFields  int
	}{
		{"oneOf", `{"type":"object","properties":{"a":{"oneOf":[{"type":"string"}]}}}`, true, true, 0},
		{"anyOf", `{"type":"object","properties":{"a":{"anyOf":[{"type":"string"}]}}}`, true, true, 0},
		{"ref", `{"type":"object","properties":{"a":{"$ref":"#/x"}}}`, true, true, 0},
		{"array", `{"type":"object","properties":{"a":{"type":"array"}}}`, true, true, 0},
		{"two-level object", `{"type":"object","properties":{"a":{"type":"object","properties":{"b":{"type":"object","properties":{"c":{"type":"string"}}}}}}}`, true, true, 0},
		{"patternProperties", `{"type":"object","properties":{"a":{"type":"object","patternProperties":{"x":{"type":"string"}}}}}`, true, true, 0},
		{"const only", `{"type":"object","properties":{"a":{"const":"x"}}}`, true, true, 0},
		{"required unsupported no default", `{"type":"object","required":["a"],"properties":{"a":{"type":"array"}}}`, false, true, 0},
		{"required unsupported with default", `{"type":"object","required":["a"],"properties":{"a":{"type":"array","default":[]}}}`, true, true, 0},
		{"mixed keeps supported", `{"type":"object","properties":{"a":{"type":"string"},"b":{"type":"array"}}}`, true, true, 1},
		{"platform skipped silently", `{"type":"object","properties":{"a":{"type":"string"},"image":{"type":"string","x-platform":true}}}`, true, false, 1},
		{"required string is fine", `{"type":"object","required":["a"],"properties":{"a":{"type":"string","minLength":3,"maxLength":30}}}`, true, false, 1},
		{"non-string enum", `{"type":"object","properties":{"a":{"enum":[1,2]}}}`, true, true, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs, err := TrimForm([]byte(tc.schema))
			if err != nil {
				t.Fatal(err)
			}
			if fs.Usable != tc.wantUsable || fs.OmittedSome != tc.wantOmitted || len(fs.Fields) != tc.wantFields {
				t.Fatalf("got usable=%v omitted=%v fields=%d; want %v %v %d",
					fs.Usable, fs.OmittedSome, len(fs.Fields), tc.wantUsable, tc.wantOmitted, tc.wantFields)
			}
		})
	}
}

func TestTitleHumanized(t *testing.T) {
	fs, _ := TrimForm([]byte(`{"type":"object","properties":{"logLevel":{"type":"string"}}}`))
	if got := fs.Fields[0].Title; got != "Log level" {
		t.Fatalf("title=%q", got)
	}
}

func TestTrimFormBadJSON(t *testing.T) {
	if _, err := TrimForm([]byte(`{`)); err == nil {
		t.Fatal("want error")
	}
}
```
`internal/catalog/catalog_test.go`

```go
package catalog_test

import (
	"strings"
	"testing"
	"testing/fstest"

	"github.com/ephematool/ephematool/internal/catalog"
	"github.com/ephematool/ephematool/templates"
)

func load(t *testing.T) *catalog.Catalog {
	t.Helper()
	c, err := catalog.Load(templates.FS) // the real embedded chart, not a copy
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestLoad(t *testing.T) {
	c := load(t)
	ts := c.Templates()
	if len(ts) != 1 || ts[0].Name != "web-app" || ts[0].Version != "0.1.0" {
		t.Fatalf("templates=%+v", ts)
	}
	if !ts[0].Form.Usable {
		t.Fatal("web-app form must be usable")
	}
	var names []string
	for _, s := range c.Sizes() {
		names = append(names, s.Name)
	}
	if strings.Join(names, ",") != "S,M,L" {
		t.Fatalf("size order=%v", names)
	}
	s, _ := c.Size("S")
	if s.LimitsCPU != "500m" || s.LimitsMemory != "512Mi" || s.DisplayCPU != "0.5 vCPU" || s.DisplayMemory != "512 MiB" {
		t.Fatalf("S=%+v", s)
	}
	l, _ := c.Size("L")
	if l.LimitsCPU != "2" || l.Helper != "Large: heavier services; uses more of the shared cluster" {
		t.Fatalf("L=%+v", l)
	}
	if _, ok := c.Size("XL"); ok {
		t.Fatal("XL must not exist")
	}
}

func TestValidateValues(t *testing.T) {
	c := load(t)
	tests := []struct {
		name       string
		values     map[string]any
		wantFields []string // exactly these, sorted
		msgHas     map[string]string
	}{
		{"valid", map[string]any{"replicas": 2, "app": map[string]any{"logLevel": "info"}}, nil, nil},
		{"empty is valid", map[string]any{}, nil, nil},
		{"replicas too big", map[string]any{"replicas": 9}, []string{"values.replicas"}, map[string]string{"values.replicas": "maximum"}},
		{"bad enum", map[string]any{"app": map[string]any{"logLevel": "loud"}}, []string{"values.app.logLevel"}, nil},
		{"extra key is summary only", map[string]any{"extra": 1}, []string{"values"}, map[string]string{"values": "additional"}},
		{"image is platform", map[string]any{"image": "x"}, []string{"values.image"}, map[string]string{"values.image": "set by the platform"}},
		{"platform block", map[string]any{"platform": map[string]any{}}, []string{"values.platform"}, map[string]string{"values.platform": "set by the platform"}},
		{"collects all", map[string]any{"replicas": 0, "containerPort": 80}, []string{"values.containerPort", "values.replicas"}, nil},
		{"wrong type", map[string]any{"replicas": "two"}, []string{"values.replicas"}, nil},
		{"float for integer", map[string]any{"replicas": 1.5}, []string{"values.replicas"}, nil},
		{"bad health path", map[string]any{"healthPath": "health"}, []string{"values.healthPath"}, nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			errs := c.ValidateValues("web-app", tc.values)
			var got []string
			for _, e := range errs {
				got = append(got, e.Field)
				if e.Code != "schema_violation" {
					t.Errorf("code=%q", e.Code)
				}
				if want, ok := tc.msgHas[e.Field]; ok && !strings.Contains(strings.ToLower(e.Message), want) {
					t.Errorf("%s message %q lacks %q", e.Field, e.Message, want)
				}
			}
			if strings.Join(got, ",") != strings.Join(tc.wantFields, ",") {
				t.Fatalf("fields=%v want %v (errs=%+v)", got, tc.wantFields, errs)
			}
		})
	}
}

func TestValidateRootErrorHasPath(t *testing.T) {
	errs := load(t).ValidateValues("web-app", map[string]any{"extra": 1})
	if len(errs) != 1 || errs[0].Path != "/" {
		t.Fatalf("errs=%+v", errs)
	}
}

func TestUnknownTemplate(t *testing.T) {
	errs := load(t).ValidateValues("nope", nil)
	if len(errs) != 1 || errs[0].Field != "template" || errs[0].Code != "unknown_template" {
		t.Fatalf("errs=%+v", errs)
	}
}

func TestLoadRejectsBrokenInput(t *testing.T) {
	good := `sizeOrder: [S]
sizes: {S: {limits: {cpu: 500m, memory: 512Mi}, requests: {cpu: 250m, memory: 256Mi}, display: {cpu: a, memory: b, helper: c}}}`
	chart := "name: x\nversion: 1.0.0\n"
	tests := map[string]fstest.MapFS{
		"missing sizes": {"x/Chart.yaml": {Data: []byte(chart)}, "x/values.schema.json": {Data: []byte(`{}`)}},
		"name mismatch": {"sizes.yaml": {Data: []byte(good)}, "x/Chart.yaml": {Data: []byte("name: y\nversion: 1\n")}, "x/values.schema.json": {Data: []byte(`{}`)}},
		"bad schema":    {"sizes.yaml": {Data: []byte(good)}, "x/Chart.yaml": {Data: []byte(chart)}, "x/values.schema.json": {Data: []byte(`{"type": 5}`)}},
		"no templates":  {"sizes.yaml": {Data: []byte(good)}},
		"size not defined": {"sizes.yaml": {Data: []byte("sizeOrder: [S, M]\nsizes: {S: {}}")},
			"x/Chart.yaml": {Data: []byte(chart)}, "x/values.schema.json": {Data: []byte(`{}`)}},
	}
	for name, fsys := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := catalog.Load(fsys); err == nil {
				t.Fatal("want error")
			}
		})
	}
}
```
```bash
bazel run //:gazelle
bazel test //internal/catalog/... && bazel build //...
```

Expected: every table case passes, including *two-level object omitted*, *required unsupported without default makes the template unusable* and *platform keys skipped silently*.

---

## 4. Preview access: ticket, cookie and ForwardAuth (plan 02-08)

A preview environment runs **untrusted PR code**, so every request to `<id>.preview.localhost:8088` is authenticated before it reaches the app (ENV-09). The flow:

1. A signed-in dashboard user opens a preview. The dashboard calls `POST /api/preview/ticket`, which returns a URL on the preview host containing a **one-time ticket** (60 s, bound to that environment, carrying the safe path to land on).
2. The browser follows it to `/__ephematool/callback?ticket=…`. Traefik asks ForwardAuth, which verifies the ticket, claims its id in Redis (`SET NX`, so it works once) and answers `302` with an **environment-bound cookie** (host-only, `HttpOnly`, `SameSite=Lax`, 30 min).
3. Every later request carries that cookie; ForwardAuth verifies it and answers `200`, replacing the `Cookie` header so the **preview app never sees its own auth cookie** (nor the dashboard session cookie, in case a client sends one anyway).
4. With no valid cookie the browser is redirected to the dashboard's authorize page to start over.

The platform session cookie never leaves the dashboard origin, so a preview cannot steal a session. Why each rule exists:

| Rule | Reason |
|---|---|
| HS256 pinned (`WithValidMethods`), `exp` required, `aud` = env id, `iss` fixed | blocks `alg:none`, algorithm confusion, tokens replayed across environments |
| `typ` claim (`ticket` vs `session`) | a cookie cannot be used as a ticket and vice versa |
| `kid` header selects current or previous key | key rotation without logging everyone out |
| the return path is **inside the signed ticket** and checked again on redemption | no open redirect, no unsigned `return` parameter |
| **all** same-named cookies are verified and **all** stripped, and `ephematool_session` is stripped too (defense in depth: browsers do not send the host-only session cookie to a preview host, a script might) | a sibling preview can plant a bad cookie with `Domain=preview.localhost` (cookie tossing); one bad cookie must not break auth, and none may reach the app |
| `__Host-` cookie name when `COOKIE_SECURE` | the browser then refuses a `Domain` attribute |
| health path exempt for exact `GET`/`HEAD` only | the prober needs no credential; nothing else is exempt |
| no signing key loaded → `503` | fails closed |
| mint endpoint answers one `403` body for unknown env, bad return and missing key | cannot be used to learn which environments exist |

### 4.1 `keys.go`: loading the signing key

The key is read from a mounted file (`PREVIEW_TOKEN_KEY_FILE`, created by `ensure_secrets` in 2a), trimmed, required to be at least 32 bytes, and never logged. The `kid` is the first 8 hex characters of its SHA-256, so it is safe in a header.

`internal/previewauth/keys.go`

```go
// Package previewauth gates every preview environment host (ENV-09): a one-time ticket,
// minted for a signed-in dashboard user, is exchanged on the preview host for a cookie
// bound to that one environment, and Traefik asks ForwardAuth about every request.
package previewauth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"

	"github.com/ephematool/ephematool/internal/config"
)

// minKeyBytes is the shortest HS256 secret we accept (the HMAC output size).
const minKeyBytes = 32

// ErrNoKeys is returned when tokens are requested but no signing key was loaded.
var ErrNoKeys = errors.New("preview token key not loaded")

type signingKey struct {
	kid    string // first 8 hex chars of sha256(secret): safe to publish in a header
	secret []byte
}

// Keys holds the current signing key and, during rotation, the previous one.
// Tokens are signed with current only; both verify (selected by the kid header).
type Keys struct {
	current  signingKey
	previous *signingKey
}

// LoadKeys reads the key files named in config. PREVIEW_TOKEN_KEY_FILE is required;
// PREVIEW_TOKEN_PREVIOUS_KEY_FILE is optional. Key material is never logged.
func LoadKeys(cfg config.Config) (*Keys, error) {
	if cfg.PreviewTokenKeyFile == "" {
		return nil, errors.New("PREVIEW_TOKEN_KEY_FILE is not set")
	}
	cur, err := readKey(cfg.PreviewTokenKeyFile)
	if err != nil {
		return nil, err
	}
	k := &Keys{current: cur}
	if cfg.PreviewTokenPreviousKeyFile != "" {
		prev, err := readKey(cfg.PreviewTokenPreviousKeyFile)
		if err != nil {
			return nil, fmt.Errorf("previous key: %w", err)
		}
		k.previous = &prev
	}
	return k, nil
}

func readKey(path string) (signingKey, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return signingKey{}, fmt.Errorf("read key file: %w", err) // path only, never contents
	}
	secret := bytes.TrimSpace(raw) // a trailing newline from `openssl rand -hex 32 > file` is not key material
	if len(secret) < minKeyBytes {
		return signingKey{}, fmt.Errorf("key file %s is shorter than %d bytes", path, minKeyBytes)
	}
	sum := sha256.Sum256(secret)
	return signingKey{kid: hex.EncodeToString(sum[:])[:8], secret: secret}, nil
}

// lookup returns the secret for a kid, or false for an unknown kid.
func (k *Keys) lookup(kid string) ([]byte, bool) {
	if k == nil {
		return nil, false
	}
	if kid == k.current.kid {
		return k.current.secret, true
	}
	if k.previous != nil && kid == k.previous.kid {
		return k.previous.secret, true
	}
	return nil, false
}
```
### 4.2 `tokens.go`

`internal/previewauth/tokens.go`

```go
package previewauth

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Token types. A ticket is single-use and short-lived; a session cookie is what the ticket
// is exchanged for. The typ claim stops one from being replayed as the other.
const (
	typTicket  = "ticket"
	typSession = "session"
	// Issuer is the iss claim on every token.
	Issuer = "ephematool"
)

// TicketClaims are the claims of both token types. Ret is set on tickets only.
type TicketClaims struct {
	jwt.RegisteredClaims
	Typ string `json:"typ"`
	Ret string `json:"ret,omitempty"` // path+query on the preview host to land on after login
}

// Tokens mints and verifies tickets and cookies.
type Tokens struct {
	Keys      *Keys
	TicketTTL time.Duration
	CookieTTL time.Duration
	Now       func() time.Time // nil means time.Now; tests inject a clock
}

func (t Tokens) now() time.Time {
	if t.Now != nil {
		return t.Now()
	}
	return time.Now()
}

// MintTicket creates a one-time ticket for envID. ret must already be a safe path (see SafeReturn).
func (t Tokens) MintTicket(envID string, githubID int64, ret string) (string, error) {
	return t.mint(envID, githubID, typTicket, ret, t.TicketTTL)
}

// MintCookie creates the environment-bound session cookie value.
func (t Tokens) MintCookie(envID string, githubID int64) (string, error) {
	return t.mint(envID, githubID, typSession, "", t.CookieTTL)
}

func (t Tokens) mint(envID string, githubID int64, typ, ret string, ttl time.Duration) (string, error) {
	if t.Keys == nil {
		return "", ErrNoKeys
	}
	jti, err := newJTI()
	if err != nil {
		return "", err
	}
	now := t.now()
	claims := TicketClaims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    Issuer,
			Audience:  jwt.ClaimStrings{envID}, // binds the token to ONE environment
			Subject:   strconv.FormatInt(githubID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
			ID:        jti,
		},
		Typ: typ,
		Ret: ret,
	}
	tok := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	tok.Header["kid"] = t.Keys.current.kid // lets verification pick current vs previous key
	return tok.SignedString(t.Keys.current.secret)
}

// VerifyTicket validates a ticket for envID and returns its claims.
func (t Tokens) VerifyTicket(tok, envID string) (TicketClaims, error) {
	return t.verify(tok, envID, typTicket)
}

// VerifyCookie validates a session cookie for envID.
func (t Tokens) VerifyCookie(tok, envID string) error {
	_, err := t.verify(tok, envID, typSession)
	return err
}

func (t Tokens) verify(tok, envID, wantTyp string) (TicketClaims, error) {
	if t.Keys == nil {
		return TicketClaims{}, ErrNoKeys
	}
	var claims TicketClaims
	_, err := jwt.ParseWithClaims(tok, &claims,
		func(tk *jwt.Token) (any, error) {
			kid, _ := tk.Header["kid"].(string)
			secret, ok := t.Keys.lookup(kid)
			if !ok {
				return nil, errors.New("unknown key id")
			}
			return secret, nil
		},
		jwt.WithValidMethods([]string{"HS256"}), // pins the algorithm: no none, no RS256 confusion
		jwt.WithAudience(envID),
		jwt.WithIssuer(Issuer),
		jwt.WithExpirationRequired(),
		jwt.WithLeeway(5*time.Second),
		jwt.WithTimeFunc(t.now),
	)
	if err != nil {
		return TicketClaims{}, err
	}
	if claims.Typ != wantTyp {
		return TicketClaims{}, fmt.Errorf("wrong token type")
	}
	return claims, nil
}

// newJTI returns a 128-bit random token id.
func newJTI() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
```
### 4.3 `safereturn.go`

`internal/previewauth/safereturn.go`

```go
package previewauth

import (
	"errors"
	"net/url"
	"strings"

	"github.com/ephematool/ephematool/internal/config"
)

const maxReturnLen = 512

var errUnsafeReturn = errors.New("unsafe return target")

// SafeReturn accepts an absolute URL that points at envID's OWN preview host and returns
// only its path and query. Anything else is rejected, so a ticket can never carry a user to
// another site (open redirect) or to a sibling environment.
func SafeReturn(cfg config.Config, envID, raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", errUnsafeReturn
	}
	if u.Scheme != cfg.PreviewScheme || u.Host != cfg.PreviewHost(envID) || u.User != nil {
		return "", errUnsafeReturn
	}
	target := u.EscapedPath()
	if target == "" {
		target = "/"
	}
	if u.RawQuery != "" {
		target += "?" + u.RawQuery
	}
	return SafePath(target)
}

// SafePath validates a path+query: one leading slash, no "//", no backslash, no control
// characters (raw or percent-encoded), and a bounded length. It is applied when a ticket is
// minted and again when it is redeemed, because the redeemer must not trust the token's
// contents blindly.
func SafePath(p string) (string, error) {
	if p == "" || p[0] != '/' || strings.HasPrefix(p, "//") || len(p) > maxReturnLen {
		return "", errUnsafeReturn
	}
	if strings.ContainsRune(p, '\\') || hasControl(p) || hasEncodedControl(p) {
		return "", errUnsafeReturn
	}
	u, err := url.Parse(p)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil {
		return "", errUnsafeReturn
	}
	// The decoded path must not hide what the raw path does not show ("/%5cevil", "/%0d%0a").
	if strings.HasPrefix(u.Path, "//") || strings.ContainsRune(u.Path, '\\') || hasControl(u.Path) {
		return "", errUnsafeReturn
	}
	return p, nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// hasEncodedControl spots percent-encoded NUL, CR and LF anywhere (header-splitting attempts).
func hasEncodedControl(s string) bool {
	l := strings.ToLower(s)
	return strings.Contains(l, "%00") || strings.Contains(l, "%0d") || strings.Contains(l, "%0a")
}
```
### 4.4 `forwardauth.go`

This is the handler Traefik's ForwardAuth middleware calls for **every** preview request. Contract: `200` allow (optionally replacing the `Cookie` header), `302` redirect the browser, anything else deny. It logs only the environment id and a coarse reason, never cookies, tickets or full URIs.

`internal/previewauth/forwardauth.go`

```go
package previewauth

import (
	"context"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/config"
)

// Paths on the preview host that the gateway routes to this handler's decisions.
const (
	// HealthPath is exempt from auth so the platform prober never needs a credential (D-05).
	HealthPath = "/__ephematool/healthz"
	// CallbackPath is where the one-time ticket is exchanged for the environment cookie.
	CallbackPath = "/__ephematool/callback"
	// AuthorizePath is the dashboard page that mints a ticket for the signed-in user.
	AuthorizePath = "/preview/authorize"
)

// CookieName is the preview cookie name. With Secure cookies the __Host- prefix makes the
// browser refuse a Domain attribute, so a sibling preview cannot plant or read it.
func CookieName(secure bool) string {
	if secure {
		return "__Host-ephematool_preview"
	}
	return "ephematool_preview"
}

// JTIStore records ticket ids so each ticket works once.
type JTIStore interface {
	// Claim returns true only the first time jti is claimed within ttl.
	Claim(ctx context.Context, jti string, ttl time.Duration) (bool, error)
}

// jtiTTL outlives the ticket TTL plus clock leeway, so an expired ticket is never replayable.
const jtiTTL = 120 * time.Second

type forwardAuth struct {
	cfg    config.Config
	tokens *Tokens
	jti    JTIStore
	host   *regexp.Regexp
}

// NewForwardAuth returns the handler Traefik's ForwardAuth middleware calls for every
// preview request. Contract: 200 allows (optionally replacing the Cookie header), 302
// redirects the browser (login or ticket exchange), anything else denies.
// It fails CLOSED: with no signing key it answers 503.
func NewForwardAuth(cfg config.Config, t *Tokens, jti JTIStore) http.Handler {
	port := ""
	if cfg.PreviewPort > 0 {
		port = `(?::` + strconv.Itoa(cfg.PreviewPort) + `)?`
	}
	re := regexp.MustCompile(`^(pv-[a-z0-9]{10})\.` + regexp.QuoteMeta(cfg.PreviewDomain) + port + `$`)
	return &forwardAuth{cfg: cfg, tokens: t, jti: jti, host: re}
}

func (f *forwardAuth) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	// 1. Host: only a real environment host shape is ever considered.
	m := f.host.FindStringSubmatch(r.Header.Get("X-Forwarded-Host"))
	if m == nil {
		f.deny(ctx, w, "", "bad_host", http.StatusForbidden)
		return
	}
	envID := m[1]

	// 2. URI: refuse anything that could be read differently here and in the app.
	uri := r.Header.Get("X-Forwarded-Uri")
	u, ok := parseForwardedURI(uri)
	if !ok {
		f.deny(ctx, w, envID, "bad_uri", http.StatusForbidden)
		return
	}
	method := r.Header.Get("X-Forwarded-Method")
	if method == "" {
		method = http.MethodGet
	}

	// 3. Health is exempt: exact path, GET/HEAD only. It grants nothing but a 2xx.
	if (method == http.MethodGet || method == http.MethodHead) && u.Path == HealthPath && !strings.Contains(uri, "%") {
		w.WriteHeader(http.StatusOK)
		return
	}

	// 4. No key, no decisions (fail closed).
	if f.tokens == nil || f.tokens.Keys == nil {
		f.deny(ctx, w, envID, "keys_unavailable", http.StatusServiceUnavailable)
		return
	}

	// 5. Ticket exchange.
	if u.Path == CallbackPath {
		f.callback(ctx, w, envID, method, u)
		return
	}

	// 6. Cookie: ALL same-named cookies are checked (a sibling could have planted a bad one
	//    ahead of the real one), and every preview cookie is stripped before the app sees it.
	if valid, rest := f.checkCookies(r, envID); valid {
		if rest != "" {
			w.Header().Set("Cookie", rest) // Traefik: authResponseHeaders=Cookie replaces the request's
		}
		w.WriteHeader(http.StatusOK)
		return
	}

	// 7. Otherwise send the browser to the dashboard to sign in and get a ticket.
	back := f.cfg.PreviewURL(envID) + uri
	loc := f.cfg.PublicBaseURL + AuthorizePath + "?env=" + url.QueryEscape(envID) + "&return=" + url.QueryEscape(back)
	slog.InfoContext(ctx, "preview.auth.redirect", "env_id", envID)
	http.Redirect(w, r, loc, http.StatusFound)
}

func (f *forwardAuth) callback(ctx context.Context, w http.ResponseWriter, envID, method string, u *url.URL) {
	if method != http.MethodGet {
		f.deny(ctx, w, envID, "callback_method", http.StatusForbidden)
		return
	}
	claims, err := f.tokens.VerifyTicket(u.Query().Get("ticket"), envID)
	if err != nil {
		f.deny(ctx, w, envID, "ticket_invalid", http.StatusForbidden) // reason is deliberately coarse
		return
	}
	first, err := f.jti.Claim(ctx, claims.ID, jtiTTL)
	if err != nil {
		f.deny(ctx, w, envID, "jti_store_error", http.StatusServiceUnavailable)
		return
	}
	if !first {
		f.deny(ctx, w, envID, "ticket_replayed", http.StatusForbidden)
		return
	}
	dest, err := SafePath(claims.Ret)
	if err != nil {
		dest = "/" // never trust the token's path blindly
	}
	githubID, _ := strconv.ParseInt(claims.Subject, 10, 64)
	val, err := f.tokens.MintCookie(envID, githubID)
	if err != nil {
		f.deny(ctx, w, envID, "mint_failed", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: CookieName(f.cfg.CookieSecure), Value: val,
		Path:   "/", // no Domain: host-only, so it never reaches sibling previews
		MaxAge: int(f.tokens.CookieTTL.Seconds()), HttpOnly: true,
		SameSite: http.SameSiteLaxMode, Secure: f.cfg.CookieSecure,
	})
	w.Header().Set("Location", dest) // relative: the browser resolves it on the preview host
	w.WriteHeader(http.StatusFound)
	slog.InfoContext(ctx, "preview.auth.granted", "env_id", envID, "github_id", githubID)
}

// checkCookies verifies every preview cookie and returns the Cookie header value with the
// preview cookies AND the platform session cookie removed, so neither ever reaches the
// previewed app. (Browsers do not send the host-only session cookie to a preview host; this
// is defense in depth for a client that does.) A malformed Cookie header counts as no valid
// cookie.
func (f *forwardAuth) checkCookies(r *http.Request, envID string) (valid bool, rest string) {
	name := CookieName(f.cfg.CookieSecure)
	var others []string
	for _, line := range r.Header.Values("Cookie") {
		cs, err := http.ParseCookie(line)
		if err != nil {
			return false, ""
		}
		for _, c := range cs {
			if c.Name == auth.CookieName {
				continue // the dashboard session is never forwarded to a preview
			}
			if c.Name != name {
				others = append(others, c.Name+"="+c.Value)
				continue
			}
			if f.tokens.VerifyCookie(c.Value, envID) == nil {
				valid = true
			}
		}
	}
	return valid, strings.Join(others, "; ")
}

// parseForwardedURI parses X-Forwarded-Uri and rejects dot-segments, encoded separators,
// double slashes in the path, backslashes and control characters.
func parseForwardedURI(uri string) (*url.URL, bool) {
	if uri == "" || uri[0] != '/' || hasControl(uri) || strings.ContainsRune(uri, '\\') {
		return nil, false
	}
	pathPart := uri
	if i := strings.IndexByte(uri, '?'); i >= 0 {
		pathPart = uri[:i]
	}
	low := strings.ToLower(pathPart)
	for _, bad := range []string{"..", "%2e", "%2f", "%5c", "//"} {
		if strings.Contains(low, bad) {
			return nil, false
		}
	}
	u, err := url.ParseRequestURI(uri)
	if err != nil {
		return nil, false
	}
	return u, true
}

func (f *forwardAuth) deny(ctx context.Context, w http.ResponseWriter, envID, reason string, status int) {
	// Never log cookies, tickets or full URIs: only the env id and a coarse reason.
	slog.WarnContext(ctx, "preview.auth.denied", "env_id", envID, "reason", reason)
	w.WriteHeader(status)
}
```
`RedisJTI` lives in its own file so the rest of the package has no Redis dependency in its tests.

`internal/previewauth/redisjti.go`

```go
package previewauth

import (
	"context"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisJTI is the production JTIStore: SET key NX EX.
type RedisJTI struct{ RDB *redis.Client }

// Claim implements JTIStore.
func (r RedisJTI) Claim(ctx context.Context, jti string, ttl time.Duration) (bool, error) {
	return r.RDB.SetNX(ctx, "preview:jti:"+jti, 1, ttl).Result()
}
```
### 4.5 `ticket.go`: the mint operation

`Register` must work with a nil `Handlers` and a nil ForwardAuth handler: OpenAPI generation and the security walk call `httpapi.New(httpapi.Deps{})`.

`internal/previewauth/ticket.go`

```go
package previewauth

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/config"
)

// EnvLookup answers whether an environment exists. The store adapter lives in httpapi.
type EnvLookup interface {
	EnvironmentExists(ctx context.Context, id string) (bool, error)
}

// Handlers carries the collaborators of the ticket mint operation.
type Handlers struct {
	Cfg      config.Config
	Tokens   *Tokens
	Envs     EnvLookup
	Sessions *scs.SessionManager
}

// TicketInput is the POST /api/preview/ticket body.
type TicketInput struct {
	Body struct {
		EnvID  string `json:"envId" pattern:"^pv-[a-z0-9]{10}$" doc:"Environment id"`
		Return string `json:"return" maxLength:"2048" doc:"Absolute URL on that environment's preview host to land on after sign-in"`
	}
}

// TicketOutput carries the URL the browser is sent to on the preview host.
type TicketOutput struct {
	Body struct {
		URL string `json:"url"`
	}
}

// denied is the ONE answer for unknown env, bad return, missing key and any other refusal, so
// the response cannot be used to learn which environments exist.
func denied() error { return huma.Error403Forbidden("access denied") }

// Register wires the mint operation into the Huma API and, when fa is not nil, the plain
// ForwardAuth route. Registration must work with a nil h and nil fa (OpenAPI generation and
// the security walk build the API with empty dependencies).
func Register(api huma.API, mux *http.ServeMux, h *Handlers, fa http.Handler, wrap auth.RouteWrapper) {
	if h == nil {
		h = &Handlers{}
	}
	if fa != nil {
		// Plain mux route outside /api/, so the dashboard's nginx front door never proxies it
		// and it is not part of the public OpenAPI document.
		mux.Handle("GET /internal/preview/auth", wrap("GET", "/internal/preview/auth", fa))
	}
	huma.Register(api, huma.Operation{
		OperationID: "post-preview-ticket", Method: http.MethodPost, Path: "/api/preview/ticket",
		Summary:  "Mint a one-time ticket to open a preview environment",
		Security: []map[string][]string{{"session": {}}},
	}, h.mint)
}

func (h *Handlers) mint(ctx context.Context, in *TicketInput) (*TicketOutput, error) {
	if h.Tokens == nil || h.Tokens.Keys == nil || h.Envs == nil {
		slog.WarnContext(ctx, "preview.ticket.denied", "reason", "not_configured")
		return nil, denied()
	}
	p, ok := auth.PrincipalFrom(ctx, h.Sessions)
	if !ok {
		return nil, denied()
	}
	envID := in.Body.EnvID
	exists, err := h.Envs.EnvironmentExists(ctx, envID)
	if err != nil || !exists {
		slog.WarnContext(ctx, "preview.ticket.denied", "env_id", envID, "reason", "unknown_env")
		return nil, denied()
	}
	ret := "/"
	if in.Body.Return != "" {
		ret, err = SafeReturn(h.Cfg, envID, in.Body.Return)
		if err != nil {
			slog.WarnContext(ctx, "preview.ticket.denied", "env_id", envID, "reason", "bad_return")
			return nil, denied()
		}
	}
	tok, err := h.Tokens.MintTicket(envID, p.GithubID, ret)
	if err != nil {
		return nil, denied()
	}
	slog.InfoContext(ctx, "preview.ticket.minted", "env_id", envID, "login", p.Login)
	out := &TicketOutput{}
	out.Body.URL = h.Cfg.PreviewURL(envID) + CallbackPath + "?ticket=" + tok
	return out, nil
}
```
### 4.6 Tests

`helpers_test.go` builds tokens by hand (`forge`) so tests can produce what the real code never would: `alg:none`, an RS256 header, HS512, a missing `exp`, an unknown `kid`.

`internal/previewauth/helpers_test.go`

```go
package previewauth

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ephematool/ephematool/internal/config"
)

const (
	envA = "pv-aaaaaaaaaa"
	envB = "pv-bbbbbbbbbb"
)

func testCfg() config.Config {
	return config.Config{
		PublicBaseURL: "http://localhost:8080", PreviewDomain: "preview.localhost",
		PreviewPort: 8088, PreviewScheme: "http",
		PreviewTicketTTL: 60 * time.Second, PreviewCookieTTL: 30 * time.Minute,
	}
}

// writeKeyFile writes content to a temp file and returns its path.
func writeKeyFile(t *testing.T, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

const (
	secret1 = "0123456789abcdef0123456789abcdef0123456789abcdef"
	secret2 = "fedcba9876543210fedcba9876543210fedcba9876543210"
)

// tokensWith builds Tokens from key files for secret (current) and optionally previous.
func tokensWith(t *testing.T, secret, previous string) Tokens {
	t.Helper()
	cfg := testCfg()
	cfg.PreviewTokenKeyFile = writeKeyFile(t, secret+"\n") // trailing newline must be trimmed
	if previous != "" {
		cfg.PreviewTokenPreviousKeyFile = writeKeyFile(t, previous)
	}
	k, err := LoadKeys(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return Tokens{Keys: k, TicketTTL: cfg.PreviewTicketTTL, CookieTTL: cfg.PreviewCookieTTL}
}

// memJTI is an in-memory JTIStore.
type memJTI struct {
	mu   sync.Mutex
	seen map[string]bool
	err  error
}

func (m *memJTI) Claim(_ context.Context, jti string, _ time.Duration) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return false, m.err
	}
	if m.seen == nil {
		m.seen = map[string]bool{}
	}
	if m.seen[jti] {
		return false, nil
	}
	m.seen[jti] = true
	return true, nil
}

// forge assembles a compact JWT by hand so tests can build tokens the real minting code
// never would (alg none, wrong alg, missing exp). sig receives "header.payload".
func forge(t *testing.T, header, claims map[string]any, sig func(signingInput string) []byte) string {
	t.Helper()
	enc := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return base64.RawURLEncoding.EncodeToString(b)
	}
	in := enc(header) + "." + enc(claims)
	var s []byte
	if sig != nil {
		s = sig(in)
	}
	return in + "." + base64.RawURLEncoding.EncodeToString(s)
}

func hs256(secret string) func(string) []byte {
	return func(in string) []byte {
		m := hmac.New(sha256.New, []byte(secret))
		m.Write([]byte(in))
		return m.Sum(nil)
	}
}

// goodClaims is a claim set that verifies as a cookie for envA; tests tweak it.
func goodClaims() map[string]any {
	return map[string]any{
		"iss": Issuer, "aud": []string{envA}, "sub": "7", "jti": "j1", "typ": "session",
		"exp": time.Now().Add(time.Minute).Unix(), "iat": time.Now().Unix(),
	}
}
```
`internal/previewauth/tokens_test.go`

```go
package previewauth

import (
	"crypto/hmac"
	"crypto/sha512"
	"strings"
	"testing"
	"time"
)

func TestTicketAndCookieRoundTrip(t *testing.T) {
	tk := tokensWith(t, secret1, "")
	ticket, err := tk.MintTicket(envA, 7, "/a?b=1")
	if err != nil {
		t.Fatal(err)
	}
	c, err := tk.VerifyTicket(ticket, envA)
	if err != nil || c.Ret != "/a?b=1" || c.Subject != "7" || c.ID == "" {
		t.Fatalf("claims=%+v err=%v", c, err)
	}
	cookie, _ := tk.MintCookie(envA, 7)
	if err := tk.VerifyCookie(cookie, envA); err != nil {
		t.Fatal(err)
	}
}

func TestTypesAreNotInterchangeable(t *testing.T) {
	tk := tokensWith(t, secret1, "")
	ticket, _ := tk.MintTicket(envA, 7, "/")
	cookie, _ := tk.MintCookie(envA, 7)
	if _, err := tk.VerifyTicket(cookie, envA); err == nil {
		t.Fatal("cookie accepted as ticket")
	}
	if err := tk.VerifyCookie(ticket, envA); err == nil {
		t.Fatal("ticket accepted as cookie")
	}
}

func TestWrongEnvironmentRejected(t *testing.T) {
	tk := tokensWith(t, secret1, "")
	cookie, _ := tk.MintCookie(envA, 7)
	if err := tk.VerifyCookie(cookie, envB); err == nil {
		t.Fatal("token for A accepted for B (audience)")
	}
}

func TestExpiry(t *testing.T) {
	past := tokensWith(t, secret1, "")
	past.Now = func() time.Time { return time.Now().Add(-time.Hour) }
	old, _ := past.MintCookie(envA, 7)
	now := tokensWith(t, secret1, "")
	if err := now.VerifyCookie(old, envA); err == nil {
		t.Fatal("expired token accepted")
	}
	// within the 5 s leeway still passes
	edge := tokensWith(t, secret1, "")
	edge.CookieTTL = time.Minute
	edge.Now = func() time.Time { return time.Now().Add(-time.Minute - 3*time.Second) }
	recent, _ := edge.MintCookie(envA, 7)
	if err := now.VerifyCookie(recent, envA); err != nil {
		t.Fatalf("leeway not applied: %v", err)
	}
}

func TestForgedTokensRejected(t *testing.T) {
	tk := tokensWith(t, secret1, "")
	kid := tk.Keys.current.kid
	good := func(h map[string]any) string { return forge(t, h, goodClaims(), hs256(secret1)) }

	// control: the forge helper itself produces a valid token
	if err := tk.VerifyCookie(good(map[string]any{"alg": "HS256", "typ": "JWT", "kid": kid}), envA); err != nil {
		t.Fatalf("helper control failed: %v", err)
	}

	noExp := goodClaims()
	delete(noExp, "exp")
	hs512 := func(in string) []byte {
		m := hmac.New(sha512.New, []byte(secret1))
		m.Write([]byte(in))
		return m.Sum(nil)
	}
	tests := map[string]string{
		"alg none":        forge(t, map[string]any{"alg": "none", "typ": "JWT", "kid": kid}, goodClaims(), nil),
		"RS256 header":    forge(t, map[string]any{"alg": "RS256", "typ": "JWT", "kid": kid}, goodClaims(), func(string) []byte { return []byte("junk") }),
		"HS512":           forge(t, map[string]any{"alg": "HS512", "typ": "JWT", "kid": kid}, goodClaims(), hs512),
		"unknown kid":     forge(t, map[string]any{"alg": "HS256", "typ": "JWT", "kid": "deadbeef"}, goodClaims(), hs256(secret1)),
		"no kid":          forge(t, map[string]any{"alg": "HS256", "typ": "JWT"}, goodClaims(), hs256(secret1)),
		"wrong secret":    forge(t, map[string]any{"alg": "HS256", "typ": "JWT", "kid": kid}, goodClaims(), hs256(secret2)),
		"missing exp":     forge(t, map[string]any{"alg": "HS256", "typ": "JWT", "kid": kid}, noExp, hs256(secret1)),
		"wrong issuer":    forge(t, map[string]any{"alg": "HS256", "kid": kid}, with(goodClaims(), "iss", "evil"), hs256(secret1)),
		"garbage":         "not.a.token",
		"empty":           "",
		"tampered claims": strings.Replace(good(map[string]any{"alg": "HS256", "kid": kid}), ".", ".x", 1),
	}
	for name, tok := range tests {
		t.Run(name, func(t *testing.T) {
			if err := tk.VerifyCookie(tok, envA); err == nil {
				t.Fatal("forged token accepted")
			}
		})
	}
}

func with(m map[string]any, k string, v any) map[string]any { m[k] = v; return m }

func TestKeyRotation(t *testing.T) {
	old := tokensWith(t, secret1, "")
	cookie, _ := old.MintCookie(envA, 7)
	rotated := tokensWith(t, secret2, secret1) // new current, old as previous
	if err := rotated.VerifyCookie(cookie, envA); err != nil {
		t.Fatalf("previous key should still verify: %v", err)
	}
	fresh, _ := rotated.MintCookie(envA, 7)
	if err := old.VerifyCookie(fresh, envA); err == nil {
		t.Fatal("old-only deployment must not accept tokens signed with a key it does not have")
	}
	if rotated.Keys.current.kid == old.Keys.current.kid {
		t.Fatal("kid must differ per key")
	}
}

func TestLoadKeysErrors(t *testing.T) {
	cfg := testCfg()
	if _, err := LoadKeys(cfg); err == nil {
		t.Fatal("unset key file must error")
	}
	cfg.PreviewTokenKeyFile = writeKeyFile(t, "short")
	if _, err := LoadKeys(cfg); err == nil {
		t.Fatal("short key must error")
	}
	cfg.PreviewTokenKeyFile = "/nonexistent/key"
	if _, err := LoadKeys(cfg); err == nil {
		t.Fatal("missing file must error")
	}
	cfg.PreviewTokenKeyFile = writeKeyFile(t, secret1)
	cfg.PreviewTokenPreviousKeyFile = writeKeyFile(t, "short")
	if _, err := LoadKeys(cfg); err == nil {
		t.Fatal("short previous key must error")
	}
}

func TestWhitespaceOnlyDoesNotCountAsKey(t *testing.T) {
	cfg := testCfg()
	cfg.PreviewTokenKeyFile = writeKeyFile(t, strings.Repeat(" ", 64)+"\n")
	if _, err := LoadKeys(cfg); err == nil {
		t.Fatal("whitespace is not key material")
	}
}

func TestMintWithoutKeys(t *testing.T) {
	if _, err := (Tokens{}).MintTicket(envA, 1, "/"); err != ErrNoKeys {
		t.Fatalf("err=%v", err)
	}
}
```
`internal/previewauth/safereturn_test.go`

```go
package previewauth

import (
	"strings"
	"testing"
)

func TestSafeReturn(t *testing.T) {
	cfg := testCfg()
	host := "http://" + envA + ".preview.localhost:8088"
	ok := map[string]string{
		host + "/a?b=1":   "/a?b=1",
		host:              "/",
		host + "/":        "/",
		host + "/x/y.css": "/x/y.css",
	}
	for in, want := range ok {
		got, err := SafeReturn(cfg, envA, in)
		if err != nil || got != want {
			t.Errorf("SafeReturn(%q)=%q,%v want %q", in, got, err, want)
		}
	}
	bad := map[string]string{
		"other env":         "http://" + envB + ".preview.localhost:8088/",
		"missing port":      "http://" + envA + ".preview.localhost/",
		"https mismatch":    "https://" + envA + ".preview.localhost:8088/",
		"other site":        "http://evil.example/",
		"userinfo":          "http://u:p@" + envA + ".preview.localhost:8088/",
		"userinfo host":     "http://" + envA + ".preview.localhost:8088@evil.example/",
		"protocol relative": host + "//evil.example",
		"backslash":         host + `/\evil`,
		"encoded backslash": host + "/%5cevil",
		"crlf encoded":      host + "/%0d%0aSet-Cookie:x=1",
		"crlf query":        host + "/?a=%0d%0a",
		"javascript":        "javascript:alert(1)",
		"relative":          "/just/a/path",
		"too long":          host + "/" + strings.Repeat("a", 600),
		"empty":             "",
	}
	for name, in := range bad {
		if got, err := SafeReturn(cfg, envA, in); err == nil {
			t.Errorf("%s: accepted %q -> %q", name, in, got)
		}
	}
}

func TestSafePath(t *testing.T) {
	for _, p := range []string{"/", "/a", "/a?b=c&d=e", "/a/b/c.js"} {
		if got, err := SafePath(p); err != nil || got != p {
			t.Errorf("SafePath(%q)=%q,%v", p, got, err)
		}
	}
	for _, p := range []string{"", "a", "//x", `/\x`, "/%0D%0A", "/a\nb", "http://x", "/%00"} {
		if _, err := SafePath(p); err == nil {
			t.Errorf("SafePath(%q) accepted", p)
		}
	}
}
```
`internal/previewauth/forwardauth_test.go`

```go
package previewauth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/ephematool/ephematool/internal/auth"
)

type faEnv struct {
	h   http.Handler
	tk  Tokens
	jti *memJTI
}

func newFA(t *testing.T) faEnv {
	t.Helper()
	tk := tokensWith(t, secret1, "")
	j := &memJTI{}
	return faEnv{h: NewForwardAuth(testCfg(), &tk, j), tk: tk, jti: j}
}

// call simulates Traefik's ForwardAuth request.
func call(h http.Handler, host, uri, method string, cookies ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(http.MethodGet, "/internal/preview/auth", nil)
	r.Header.Set("X-Forwarded-Host", host)
	r.Header.Set("X-Forwarded-Uri", uri)
	if method != "" {
		r.Header.Set("X-Forwarded-Method", method)
	}
	for _, c := range cookies {
		r.Header.Add("Cookie", c)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

const hostA = envA + ".preview.localhost:8088"

func TestHealthExemption(t *testing.T) {
	f := newFA(t)
	tests := []struct {
		name, uri, method string
		want              int
	}{
		{"GET", HealthPath, "GET", 200},
		{"HEAD", HealthPath, "HEAD", 200},
		{"default method", HealthPath, "", 200},
		{"POST not exempt", HealthPath, "POST", 302},
		{"trailing slash not exempt", HealthPath + "/", "GET", 302},
		{"dot segments rejected", "/x/../__ephematool/healthz", "GET", 403},
		{"encoded not exempt", "/__ephematool/h%65althz", "GET", 302},
		{"other path", "/health", "GET", 302},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := call(f.h, hostA, tc.uri, tc.method).Code; got != tc.want {
				t.Fatalf("code=%d want %d", got, tc.want)
			}
		})
	}
}

func TestBadHostAndURI(t *testing.T) {
	f := newFA(t)
	for _, host := range []string{
		"evil.example", "pv-abc.preview.localhost:8088", envA + ".preview.localhost.evil",
		strings.ToUpper(hostA), envA + ".preview.localhost:9999", "", "xpv-aaaaaaaaaa.preview.localhost",
	} {
		if got := call(f.h, host, "/", "GET").Code; got != 403 {
			t.Errorf("host %q -> %d, want 403", host, got)
		}
	}
	for _, uri := range []string{"", "x", "//evil", "/a/../b", "/a%2e%2e/b", "/a%2fb", "/a%5cb", `/a\b`, "/a\nb", "/a//b"} {
		if got := call(f.h, hostA, uri, "GET").Code; got != 403 {
			t.Errorf("uri %q -> %d, want 403", uri, got)
		}
	}
	// "//" in the QUERY is legitimate (a URL passed as a parameter) and must not be refused.
	if got := call(f.h, hostA, "/go?next=http://x/y", "GET").Code; got != 302 {
		t.Errorf("query with // -> %d, want 302 (login redirect)", got)
	}
}

func TestNoCookieRedirectsToAuthorize(t *testing.T) {
	f := newFA(t)
	w := call(f.h, hostA, "/", "GET")
	want := "http://localhost:8080/preview/authorize?env=" + envA +
		"&return=" + url.QueryEscape("http://"+hostA+"/")
	if w.Code != 302 || w.Header().Get("Location") != want {
		t.Fatalf("code=%d location=%q want %q", w.Code, w.Header().Get("Location"), want)
	}
	// the return value is built from the validated host, not echoed from a header
	w = call(f.h, hostA, "/p?q=1", "GET")
	if !strings.Contains(w.Header().Get("Location"), url.QueryEscape("http://"+hostA+"/p?q=1")) {
		t.Fatalf("location=%q", w.Header().Get("Location"))
	}
}

func TestCallbackFlow(t *testing.T) {
	f := newFA(t)
	ticket, _ := f.tk.MintTicket(envA, 7, "/a?b=1")
	uri := CallbackPath + "?ticket=" + ticket

	w := call(f.h, hostA, uri, "GET")
	if w.Code != 302 || w.Header().Get("Location") != "/a?b=1" {
		t.Fatalf("code=%d loc=%q", w.Code, w.Header().Get("Location"))
	}
	cs := w.Result().Cookies()
	if len(cs) != 1 {
		t.Fatalf("cookies=%v", cs)
	}
	c := cs[0]
	if c.Name != "ephematool_preview" || !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/" || c.Domain != "" {
		t.Fatalf("cookie flags wrong: %+v", c)
	}
	if c.MaxAge != 1800 || c.Secure {
		t.Fatalf("maxage=%d secure=%v", c.MaxAge, c.Secure)
	}

	// the cookie it set opens the environment
	if got := call(f.h, hostA, "/", "GET", c.Name+"="+c.Value).Code; got != 200 {
		t.Fatalf("cookie not accepted: %d", got)
	}
	// replay: same ticket again
	if got := call(f.h, hostA, uri, "GET").Code; got != 403 {
		t.Fatalf("replay -> %d, want 403", got)
	}
	// that cookie is bound to envA only
	hostB := envB + ".preview.localhost:8088"
	if got := call(f.h, hostB, "/", "GET", c.Name+"="+c.Value).Code; got != 302 {
		t.Fatalf("cookie for A accepted on B: %d", got)
	}
}

func TestCallbackRejections(t *testing.T) {
	f := newFA(t)
	other, _ := f.tk.MintTicket(envB, 7, "/") // ticket for another environment
	cookie, _ := f.tk.MintCookie(envA, 7)     // cookie used as a ticket
	tests := map[string]struct{ uri, method string }{
		"other env":    {CallbackPath + "?ticket=" + other, "GET"},
		"cookie typ":   {CallbackPath + "?ticket=" + cookie, "GET"},
		"no ticket":    {CallbackPath, "GET"},
		"garbage":      {CallbackPath + "?ticket=abc", "GET"},
		"POST method":  {CallbackPath + "?ticket=" + other, "POST"},
		"empty ticket": {CallbackPath + "?ticket=", "GET"},
	}
	for name, tc := range tests {
		if got := call(f.h, hostA, tc.uri, tc.method).Code; got != 403 {
			t.Errorf("%s -> %d, want 403", name, got)
		}
	}
}

func TestCallbackNeverRedirectsOffHost(t *testing.T) {
	f := newFA(t)
	// A ticket whose ret was tampered into something unsafe cannot be minted through the API,
	// but the redeemer must not trust it anyway.
	bad, _ := f.tk.MintTicket(envA, 7, "//evil.example")
	w := call(f.h, hostA, CallbackPath+"?ticket="+bad, "GET")
	if w.Code != 302 || w.Header().Get("Location") != "/" {
		t.Fatalf("code=%d loc=%q, want 302 to /", w.Code, w.Header().Get("Location"))
	}
}

func TestJTIStoreFailureFailsClosed(t *testing.T) {
	f := newFA(t)
	f.jti.err = errors.New("redis down")
	ticket, _ := f.tk.MintTicket(envA, 7, "/")
	if got := call(f.h, hostA, CallbackPath+"?ticket="+ticket, "GET").Code; got != 503 {
		t.Fatalf("code=%d want 503", got)
	}
}

func TestCookieHandling(t *testing.T) {
	f := newFA(t)
	valid, _ := f.tk.MintCookie(envA, 7)
	name := "ephematool_preview"

	// valid cookie + app cookie: allowed, Cookie header replaced with the app cookie only
	w := call(f.h, hostA, "/", "GET", name+"="+valid+"; app=1")
	if w.Code != 200 || w.Header().Get("Cookie") != "app=1" {
		t.Fatalf("code=%d cookie=%q", w.Code, w.Header().Get("Cookie"))
	}
	// two same-named cookies (a planted bad one first): still allowed, BOTH stripped
	w = call(f.h, hostA, "/", "GET", name+"=garbage; "+name+"="+valid+"; app=1")
	if w.Code != 200 || w.Header().Get("Cookie") != "app=1" {
		t.Fatalf("code=%d cookie=%q", w.Code, w.Header().Get("Cookie"))
	}
	// the platform session cookie is stripped too (defense in depth); an app cookie survives
	w = call(f.h, hostA, "/", "GET", name+"="+valid+"; "+auth.CookieName+"=leak; app=1")
	if w.Code != 200 || w.Header().Get("Cookie") != "app=1" {
		t.Fatalf("session cookie forwarded: code=%d cookie=%q", w.Code, w.Header().Get("Cookie"))
	}
	// cookies across separate Cookie headers are also all checked
	w = call(f.h, hostA, "/", "GET", name+"=garbage", name+"="+valid, "a=1")
	if w.Code != 200 || w.Header().Get("Cookie") != "a=1" {
		t.Fatalf("code=%d cookie=%q", w.Code, w.Header().Get("Cookie"))
	}
	// only the preview cookie: allowed and no Cookie header value is returned
	w = call(f.h, hostA, "/", "GET", name+"="+valid)
	if w.Code != 200 || w.Header().Get("Cookie") != "" {
		t.Fatalf("code=%d cookie=%q", w.Code, w.Header().Get("Cookie"))
	}
	// only garbage: login redirect
	if got := call(f.h, hostA, "/", "GET", name+"=garbage; app=1").Code; got != 302 {
		t.Fatalf("garbage -> %d", got)
	}
	// a session cookie for another env must not open this one
	other, _ := f.tk.MintCookie(envB, 7)
	if got := call(f.h, hostA, "/", "GET", name+"="+other).Code; got != 302 {
		t.Fatalf("cookie for B -> %d", got)
	}
}

func TestSecureCookiesUseHostPrefix(t *testing.T) {
	cfg := testCfg()
	cfg.CookieSecure = true
	tk := tokensWith(t, secret1, "")
	h := NewForwardAuth(cfg, &tk, &memJTI{})
	ticket, _ := tk.MintTicket(envA, 7, "/")
	w := call(h, hostA, CallbackPath+"?ticket="+ticket, "GET")
	cs := w.Result().Cookies()
	if len(cs) != 1 || cs[0].Name != "__Host-ephematool_preview" || !cs[0].Secure || cs[0].Domain != "" || cs[0].Path != "/" {
		t.Fatalf("cookies=%+v", cs)
	}
}

func TestFailsClosedWithoutKeys(t *testing.T) {
	for name, h := range map[string]http.Handler{
		"nil tokens": NewForwardAuth(testCfg(), nil, &memJTI{}),
		"nil keys":   NewForwardAuth(testCfg(), &Tokens{}, &memJTI{}),
	} {
		if got := call(h, hostA, "/", "GET").Code; got != 503 {
			t.Errorf("%s: code=%d want 503", name, got)
		}
		// health stays reachable: it grants nothing, and the platform prober needs it
		if got := call(h, hostA, HealthPath, "GET").Code; got != 200 {
			t.Errorf("%s: health=%d want 200", name, got)
		}
	}
}

func TestDefaultPortOmitted(t *testing.T) {
	cfg := testCfg()
	cfg.PreviewPort = 80
	tk := tokensWith(t, secret1, "")
	h := NewForwardAuth(cfg, &tk, &memJTI{})
	if got := call(h, envA+".preview.localhost", "/", "GET").Code; got != 302 {
		t.Fatalf("code=%d", got)
	}
}
```
`internal/previewauth/ticket_test.go`

```go
package previewauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/redis/go-redis/v9"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/config"
)

type stubEnvs map[string]bool

func (s stubEnvs) EnvironmentExists(_ context.Context, id string) (bool, error) { return s[id], nil }

type ticketHarness struct {
	h      http.Handler
	cookie *http.Cookie
	tokens Tokens
}

func newTicketHarness(t *testing.T, withKeys bool) *ticketHarness {
	t.Helper()
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	sm := auth.NewSessionManager(rdb, config.Config{SessionIdleTimeout: time.Hour, SessionLifetime: 2 * time.Hour})

	tk := Tokens{TicketTTL: time.Minute, CookieTTL: time.Hour}
	if withKeys {
		tk = tokensWith(t, secret1, "")
	}
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "0"))
	api.UseMiddleware(auth.RequireSession(api, sm)) // same secure-by-default middleware as production
	mux.HandleFunc("POST /test/login", func(w http.ResponseWriter, r *http.Request) {
		_ = sm.RenewToken(r.Context())
		sm.Put(r.Context(), auth.KeyUserID, int64(42))
		sm.Put(r.Context(), auth.KeyLogin, "octo")
	})
	Register(api, mux, &Handlers{Cfg: testCfg(), Tokens: &tk, Envs: stubEnvs{envA: true}, Sessions: sm}, nil, nil)

	hh := sm.LoadAndSave(mux)
	rec := httptest.NewRecorder()
	hh.ServeHTTP(rec, httptest.NewRequest("POST", "/test/login", nil))
	var c *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == auth.CookieName {
			c = ck
		}
	}
	if c == nil {
		t.Fatal("login set no session cookie")
	}
	return &ticketHarness{h: hh, cookie: c, tokens: tk}
}

func (th *ticketHarness) post(body string, withSession bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/api/preview/ticket", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	if withSession {
		req.AddCookie(th.cookie)
	}
	rec := httptest.NewRecorder()
	th.h.ServeHTTP(rec, req)
	return rec
}

func TestMintTicket(t *testing.T) {
	th := newTicketHarness(t, true)
	rec := th.post(`{"envId":"`+envA+`","return":"http://`+hostA+`/a?b=1"}`, true)
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	body := rec.Body.String()
	prefix := "http://" + hostA + CallbackPath + "?ticket="
	i := strings.Index(body, prefix)
	if i < 0 {
		t.Fatalf("url not found in %s", body)
	}
	tok := body[i+len(prefix):]
	tok = tok[:strings.IndexByte(tok, '"')]
	c, err := th.tokens.VerifyTicket(tok, envA)
	if err != nil || c.Ret != "/a?b=1" || c.Subject != "42" {
		t.Fatalf("claims=%+v err=%v", c, err)
	}
}

func TestMintDefaultsReturnToRoot(t *testing.T) {
	th := newTicketHarness(t, true)
	rec := th.post(`{"envId":"`+envA+`","return":""}`, true)
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
}

func TestMintRefusalsShareOneBody(t *testing.T) {
	th := newTicketHarness(t, true)
	unknown := th.post(`{"envId":"`+envB+`","return":""}`, true)
	badReturn := th.post(`{"envId":"`+envA+`","return":"http://evil.example/"}`, true)
	noKeys := newTicketHarness(t, false).post(`{"envId":"`+envA+`","return":""}`, true)
	for name, rec := range map[string]*httptest.ResponseRecorder{"unknown": unknown, "badReturn": badReturn, "noKeys": noKeys} {
		if rec.Code != 403 {
			t.Errorf("%s: code=%d want 403", name, rec.Code)
		}
	}
	if unknown.Body.String() != badReturn.Body.String() || unknown.Body.String() != noKeys.Body.String() {
		t.Fatalf("bodies differ (would leak which env exists):\n%s\n%s\n%s", unknown.Body, badReturn.Body, noKeys.Body)
	}
}

func TestMintRequiresSession(t *testing.T) {
	th := newTicketHarness(t, true)
	if got := th.post(`{"envId":"`+envA+`","return":""}`, false).Code; got != 401 {
		t.Fatalf("code=%d want 401", got)
	}
}

func TestMintRejectsMalformedEnvID(t *testing.T) {
	th := newTicketHarness(t, true)
	if got := th.post(`{"envId":"nope","return":""}`, true).Code; got != 422 {
		t.Fatalf("code=%d want 422 (schema pattern)", got)
	}
}

func TestRegisterIsNilSafe(t *testing.T) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "0"))
	Register(api, mux, nil, nil, nil) // OpenAPI generation and the security walk do exactly this
}
```
```bash
go get github.com/golang-jwt/jwt/v5@v5.3.1 && bazel mod tidy      # already added in 2a
bazel run //:gazelle
bazel test //internal/previewauth/...
```

---

## 5. Environment telemetry and the sample workload (plan 02-09)

Preview workloads emit OTLP too, but they run untrusted code, so their telemetry gets its **own** receiver, pipelines, limiter and exporters, separate from the platform's.

What the config does, and what it honestly cannot do:

- **Accepts only HTTP on `:4320`**, with a 1 MiB body cap. No gRPC: smaller surface.
- **Drops** anything without an environment id, and anything claiming `service.namespace=ephematool` (a preview pretending to be the platform).
- The key name comes from `${env:EPHEMATOOL_ATTR_ENV_ID}` (the compose `env_file` is the generated conventions file), so no hand-written key exists in `deploy/`.
- **A lower memory limit** (300 MiB against the platform's 400): limiters measure the whole collector process, so under pressure environment traffic is refused first.
- **Separate exporters with bounded queues**, so a slow environment export cannot block platform exports.
- **No rate limiter.** collector-contrib has none; the protection is the body cap, bounded batches and queues and the lower memory threshold. Say so when asked.
- **Identity is client-asserted.** A malicious preview can claim another environment's id inside these pipelines. Accepted for Phase 2; Phase 6 stamps identity from namespace labels.
- Environment attributes stay **resource attributes** (they appear in `target_info`), never Prometheus series labels, to avoid a cardinality explosion.

### 5.1 `deploy/otel/collector.yaml` (replace)

`deploy/otel/collector.yaml`

```yaml
receivers:
  otlp/platform:
    protocols:
      grpc:
        endpoint: 0.0.0.0:4317
      http:
        endpoint: 0.0.0.0:4318
  # Preview-environment workloads (untrusted PR code) send here. HTTP only: a smaller attack
  # surface than gRPC, and the body cap bounds what one request can cost.
  otlp/environments:
    protocols:
      http:
        endpoint: 0.0.0.0:4320
        max_request_body_size: 1048576

processors:
  memory_limiter:
    check_interval: 1s
    limit_mib: 400
    spike_limit_mib: 100
  # A memory_limiter measures the WHOLE collector process, not its own pipeline. So the
  # environments limiter has a LOWER threshold than the platform's (300 < 400): under memory
  # pressure environment telemetry is refused first and platform telemetry keeps flowing.
  memory_limiter/environments:
    check_interval: 1s
    limit_mib: 300
    spike_limit_mib: 50
  resource/platform:
    attributes:
      - key: deployment.environment.name
        value: local
        action: upsert
  attributes/redact:
    actions:
      - key: http.request.header.authorization
        action: delete
      - key: http.request.header.cookie
        action: delete
      - key: http.response.header.set-cookie
        action: delete
  # Drops (a) anything without an environment id and (b) anything claiming to be the platform.
  # The id key name comes from environments/conventions.gen.env (compose env_file) so no
  # hand-written ephematool-prefixed key appears here (the conventions gate scans deploy/).
  # HONEST LIMITS: identity is CLIENT-ASSERTED. A malicious preview can still claim another
  # environment's id inside these pipelines (accepted for Phase 2; Phase 6 stamps identity from
  # namespace labels). And collector-contrib has no rate limiter, so flood protection is the
  # body cap, bounded batches and queues, and the lower memory threshold above.
  filter/environments:
    error_mode: ignore
    traces:
      span:
        - 'resource.attributes["${env:EPHEMATOOL_ATTR_ENV_ID}"] == nil'
        - 'resource.attributes["service.namespace"] == "ephematool"'
    metrics:
      metric:
        - 'resource.attributes["${env:EPHEMATOOL_ATTR_ENV_ID}"] == nil'
        - 'resource.attributes["service.namespace"] == "ephematool"'
    logs:
      log_record:
        - 'resource.attributes["${env:EPHEMATOOL_ATTR_ENV_ID}"] == nil'
        - 'resource.attributes["service.namespace"] == "ephematool"'
  resource/environments:
    attributes:
      - key: deployment.environment.name
        value: preview
        action: upsert
      - key: telemetry.source
        value: environment
        action: upsert
  batch: {}
  batch/environments:
    send_batch_size: 512
    send_batch_max_size: 1024
    timeout: 5s

exporters:
  otlp/tempo:
    endpoint: tempo:4317
    tls:
      insecure: true
  otlphttp/loki:
    endpoint: http://loki:3100/otlp
  otlphttp/prometheus:
    endpoint: http://prometheus:9090/api/v1/otlp
  # Separate exporter instances for environments, each with a bounded queue, so a slow or
  # flooded environment export can never block platform exports.
  otlp/tempo-env:
    endpoint: tempo:4317
    tls:
      insecure: true
    sending_queue:
      queue_size: 500
  otlphttp/loki-env:
    endpoint: http://loki:3100/otlp
    sending_queue:
      queue_size: 500
  otlphttp/prometheus-env:
    endpoint: http://prometheus:9090/api/v1/otlp
    sending_queue:
      queue_size: 500

extensions:
  health_check:
    endpoint: 0.0.0.0:13133

service:
  extensions: [health_check]
  telemetry:
    metrics:
      level: normal
      readers:
        - pull:
            exporter:
              prometheus:
                host: 0.0.0.0
                port: 8888
  pipelines:
    traces/platform:
      receivers: [otlp/platform]
      processors: [memory_limiter, resource/platform, attributes/redact, batch]
      exporters: [otlp/tempo]
    metrics/platform:
      receivers: [otlp/platform]
      processors: [memory_limiter, resource/platform, batch]
      exporters: [otlphttp/prometheus]
    logs/platform:
      receivers: [otlp/platform]
      processors: [memory_limiter, resource/platform, attributes/redact, batch]
      exporters: [otlphttp/loki]
    traces/environments:
      receivers: [otlp/environments]
      processors: [memory_limiter/environments, filter/environments, resource/environments, attributes/redact, batch/environments]
      exporters: [otlp/tempo-env]
    metrics/environments:
      receivers: [otlp/environments]
      processors: [memory_limiter/environments, filter/environments, resource/environments, batch/environments]
      exporters: [otlphttp/prometheus-env]
    logs/environments:
      receivers: [otlp/environments]
      processors: [memory_limiter/environments, filter/environments, resource/environments, attributes/redact, batch/environments]
      exporters: [otlphttp/loki-env]
```
### 5.2 `deploy/compose.yaml` (edit: the `otel-collector` service)

Replace the service's `volumes:` and `ports:` with:

```yaml
    # The environments pipeline reads the attribute key names from the generated conventions
    # file (collector.yaml uses ${env:EPHEMATOOL_ATTR_ENV_ID}); nothing is hand-written there.
    env_file:
      - ../environments/conventions.gen.env
    volumes:
      - ./otel/collector.yaml:/etc/otelcol/config.yaml:ro
    ports:
      # health_check extension (13133) and the environments OTLP ingest (4320), both bound to
      # loopback and never proxied by nginx. The platform OTLP ports stay unpublished (T-01-03).
      - "127.0.0.1:13133:13133"
      - "127.0.0.1:4320:4320"
```

```bash
docker compose -f deploy/compose.yaml config -q && echo OK
```

On Docker Desktop for Mac, pods reach a host service bound to `127.0.0.1` through `host.docker.internal`. On Linux Docker that name needs `0.0.0.0` or the bridge address (not verified here).

### 5.3 The collector check script

`scripts/verify.d/env-telemetry.sh`

```bash
#!/usr/bin/env bash
# Proves the environments collector pipelines: valid config, accept with an env id,
# drop without one, drop a platform-identity spoof. Needs the stack: bazel run //:up_fake
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
. "$ROOT/scripts/lib/verify-lib.sh"
# Key names come from the generated file; nothing here is hand-written (conventions gate).
set -a; . "$ROOT/environments/conventions.gen.env"; set +a
ENV_ID_KEY="$EPHEMATOOL_ATTR_ENV_ID"
OWNER_KEY="$EPHEMATOOL_ATTR_OWNER"
TEMPLATE_KEY="$EPHEMATOOL_ATTR_TEMPLATE"
COLLECTOR_IMAGE="${COLLECTOR_IMAGE:-otel/opentelemetry-collector-contrib:0.162.0}"

# 1. The config must validate with the env file (a bad OTTL line fails here, not at runtime).
docker run --rm --env-file "$ROOT/environments/conventions.gen.env" \
  -v "$ROOT/deploy/otel:/cfg:ro" "$COLLECTOR_IMAGE" validate --config=/cfg/collector.yaml \
  >/dev/null 2>"$VERIFY_TMP/validate.err" || { cat "$VERIFY_TMP/validate.err" >&2; fail "collector config does not validate"; }
pass "collector config validates"

# 2. Send three traces to the environments receiver.
rand_hex() { head -c "$1" /dev/urandom | od -An -tx1 | tr -d ' \n'; }
send_trace() { # send_trace TRACE_ID RESOURCE_ATTRS_JSON
  local now; now="$(date +%s)000000000"
  curl -fsS -o /dev/null -X POST "http://127.0.0.1:4320/v1/traces" -H 'Content-Type: application/json' -d @- <<JSON
{"resourceSpans":[{"resource":{"attributes":[$2]},"scopeSpans":[{"scope":{"name":"env-telemetry-check"},"spans":[{"traceId":"$1","spanId":"$(rand_hex 8)","name":"check","kind":1,"startTimeUnixNano":"$now","endTimeUnixNano":"$now"}]}]}]}
JSON
}
attr() { printf '{"key":"%s","value":{"stringValue":"%s"}}' "$1" "$2"; }

good="$(rand_hex 16)"; noid="$(rand_hex 16)"; spoof="$(rand_hex 16)"
send_trace "$good"  "$(attr service.name envcheck),$(attr "$ENV_ID_KEY" pv-telemetry01),$(attr "$OWNER_KEY" octo-admin),$(attr "$TEMPLATE_KEY" web-app)"
send_trace "$noid"  "$(attr service.name envcheck)"
send_trace "$spoof" "$(attr service.name envcheck),$(attr "$ENV_ID_KEY" pv-telemetry01),$(attr service.namespace ephematool)"

# 3. The valid one reaches Tempo with its three attributes; the other two never do.
wait_found() { # wait_found TRACE_ID TIMEOUT -> 0 when Tempo returns the trace
  local i=0; while [ "$i" -lt "$2" ]; do
    curl -fsS --max-time 5 "$TEMPO_URL/api/traces/$1" -o "$VERIFY_TMP/t.json" 2>/dev/null && return 0
    sleep 1; i=$((i + 1)); done; return 1; }
wait_found "$good" 45 || fail "trace with an env id never reached Tempo"
for want in "$ENV_ID_KEY" "$OWNER_KEY" "$TEMPLATE_KEY"; do
  grep -q "\"$want\"" "$VERIFY_TMP/t.json" || fail "accepted trace lacks attribute $want"
done
pass "trace with env id arrived tagged with id, owner and template"
wait_found "$noid" 8  && fail "trace WITHOUT an env id was accepted"
wait_found "$spoof" 8 && fail "trace claiming service.namespace=ephematool was accepted"
pass "id-less and platform-spoofing traces were dropped"
```
```bash
chmod +x scripts/verify.d/env-telemetry.sh
bash -n scripts/verify.d/env-telemetry.sh && echo syntax-ok
bash scripts/verify.d/conventions.sh        # PASS: no hand-written D-09 prefixes in deploy/ or scripts/
# with the stack running (bazel run //:up_fake):
bash scripts/verify.d/env-telemetry.sh      # PASS x3: validates, accepts with id, drops the other two
```

> **Check the validator can fail.** Put a deliberately broken OTTL expression in `filter/environments`, run the script (step 1 must fail), then revert.

### 5.4 `cmd/sampleapp`: the reference preview workload

It serves `/health`, echoes the request headers (so a test can prove the gateway stripped the preview cookie), and configures OpenTelemetry **only** from `OTEL_*` variables. It must not use `internal/telemetry.Setup`: that sets `service.namespace=ephematool`, which the filter drops. The environment id, owner and template reach every span through `OTEL_RESOURCE_ATTRIBUTES`, which the chart sets from values the runner builds.

`cmd/sampleapp/main.go`

```go
// Command sampleapp is the reference preview workload: it serves /health, echoes the request
// headers it receives (so tests can prove the gateway stripped the preview cookie), and exports
// OTLP telemetry configured ONLY through the standard OTEL_* environment variables.
//
// It deliberately does NOT use internal/telemetry.Setup: that sets service.namespace=ephematool,
// which the environments collector pipeline drops as a platform-identity spoof.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "fatal:", err)
		os.Exit(1)
	}
}

// newMux returns the app's routes; tests call it directly.
func newMux() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("ok"))
	})
	// Everything else echoes what the app received, including Cookie.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"path": r.URL.Path, "headers": r.Header})
	})
	return mux
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	shutdown, err := setupTelemetry(ctx)
	if err != nil {
		return err
	}
	defer func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = shutdown(c)
	}()

	// One span per request, named by a bounded route ("GET /health" or "GET /"), never the raw path.
	handler := otelhttp.NewHandler(newMux(), "sampleapp",
		otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
			if r.URL.Path == "/health" {
				return r.Method + " /health"
			}
			return r.Method + " /"
		}))

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}
	srv := &http.Server{
		Addr: ":" + port, Handler: handler,
		ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second,
		WriteTimeout: 30 * time.Second, IdleTimeout: 120 * time.Second,
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("sampleapp listening", "addr", srv.Addr)

	select {
	case err := <-errc:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(c)
	}
	return nil
}

// setupTelemetry installs OTLP/HTTP exporters when OTEL_EXPORTER_OTLP_ENDPOINT is set, and does
// nothing otherwise (the app must run fine with no collector). The resource comes from
// resource.Default(), which honors OTEL_RESOURCE_ATTRIBUTES and OTEL_SERVICE_NAME; that is how
// the environment id, owner and template reach every span without this code knowing them.
func setupTelemetry(ctx context.Context) (func(context.Context) error, error) {
	if os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT") == "" {
		return func(context.Context) error { return nil }, nil
	}
	res := resource.Default()

	te, err := otlptracehttp.New(ctx) // endpoint and protocol come from OTEL_* env vars
	if err != nil {
		return nil, err
	}
	tp := sdktrace.NewTracerProvider(sdktrace.WithBatcher(te), sdktrace.WithResource(res))
	otel.SetTracerProvider(tp)

	me, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, err
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(me, sdkmetric.WithInterval(10*time.Second))),
		sdkmetric.WithResource(res))
	otel.SetMeterProvider(mp)

	return func(c context.Context) error {
		return errors.Join(tp.Shutdown(c), mp.Shutdown(c))
	}, nil
}
```
`cmd/sampleapp/main_test.go`

```go
package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealth(t *testing.T) {
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, httptest.NewRequest("GET", "/health", nil))
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("code=%d body=%q", w.Code, w.Body)
	}
}

func TestEchoesHeadersIncludingCookie(t *testing.T) {
	r := httptest.NewRequest("GET", "/anything", nil)
	r.Header.Set("Cookie", "app=1")
	w := httptest.NewRecorder()
	newMux().ServeHTTP(w, r)
	var got struct {
		Path    string              `json:"path"`
		Headers map[string][]string `json:"headers"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != http.StatusOK || got.Path != "/anything" || len(got.Headers["Cookie"]) != 1 || got.Headers["Cookie"][0] != "app=1" {
		t.Fatalf("code=%d got=%+v", w.Code, got)
	}
}

func TestSetupTelemetryDisabledWithoutEndpoint(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	shutdown, err := setupTelemetry(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := shutdown(t.Context()); err != nil {
		t.Fatal(err)
	}
}
```
`cmd/sampleapp/BUILD.bazel`

```python
load("@io_bazel_rules_go//go:def.bzl", "go_binary", "go_library", "go_test")

go_library(
    name = "sampleapp_lib",
    srcs = ["main.go"],
    importpath = "github.com/ephematool/ephematool/cmd/sampleapp",
    visibility = ["//visibility:private"],
    deps = [
        "@io_opentelemetry_go_contrib_instrumentation_net_http_otelhttp//:otelhttp",
        "@io_opentelemetry_go_otel//:otel",
        "@io_opentelemetry_go_otel_exporters_otlp_otlpmetric_otlpmetrichttp//:otlpmetrichttp",
        "@io_opentelemetry_go_otel_exporters_otlp_otlptrace_otlptracehttp//:otlptracehttp",
        "@io_opentelemetry_go_otel_sdk//resource",
        "@io_opentelemetry_go_otel_sdk//trace",
        "@io_opentelemetry_go_otel_sdk_metric//:metric",
    ],
)

go_binary(
    name = "sampleapp",
    embed = [":sampleapp_lib"],
    visibility = ["//visibility:public"],
)

go_test(
    name = "sampleapp_test",
    srcs = ["main_test.go"],
    embed = [":sampleapp_lib"],
)
```
```bash
bazel run //:gazelle        # rewrites the deps list to the exact labels in your MODULE.bazel
bazel test //cmd/sampleapp/... && bazel build //cmd/sampleapp
```

---

## 6. Queue additions (plan 02-10, task 1)

The create endpoint publishes `env:dispatch` on a dedicated queue. The worker (2c) serves it with concurrency **1**: GitHub asks that dispatch POSTs be serial and about a second apart, and a dispatch is not idempotent. The payload is only the environment id; everything else is read from the database, so a task cannot carry stale or attacker-chosen data.

### 6.1 `internal/queue/tasks.go` (replace)

`internal/queue/tasks.go`

```go
// Package queue holds asynq task types and trace-context propagation.
package queue

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

// Task type names.
const (
	TypePing      = "telemetry:ping"
	TypeHeartbeat = "telemetry:heartbeat"

	// Environment lifecycle tasks (Phase 2). The handlers arrive with the worker in phase 2c.
	TypeEnvDispatch = "env:dispatch" // POST the workflow dispatch, exactly once
	TypeEnvPoll     = "env:poll"     // follow the Actions run until it concludes
	TypeEnvProbe    = "env:probe"    // wait for the preview to answer its health path
	TypeEnvSweep    = "env:sweep"    // re-enqueue anything a restart or Redis flush dropped
)

// QueueDispatch is served with concurrency 1: GitHub asks for POSTs to be serial and about one
// second apart, and a dispatch is not idempotent.
const QueueDispatch = "dispatch"

// EnvPayload is the body of every env:* task. Everything else is read from the database, so a
// task can never carry stale or attacker-chosen data.
type EnvPayload struct {
	EnvID string `json:"env_id"`
}

// InjectHeaders serialises the W3C trace context from ctx into a header map
// suitable for asynq.NewTaskWithHeaders.
func InjectHeaders(ctx context.Context) map[string]string {
	h := map[string]string{}
	otel.GetTextMapPropagator().Inject(ctx, propagation.MapCarrier(h))
	return h
}
```
### 6.2 `internal/queue/client.go` (replace)

`internal/queue/client.go`

```go
package queue

import (
	"context"
	"encoding/json"

	"github.com/hibiken/asynq"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// Client wraps an asynq producer. It owns its own Redis connection, which is
// deliberately not instrumented (Pitfall 4).
type Client struct {
	c *asynq.Client
}

// NewClient connects an asynq client to redisAddr.
func NewClient(redisAddr string) *Client {
	return &Client{c: asynq.NewClient(asynq.RedisClientOpt{Addr: redisAddr})}
}

// Close releases the Redis connection.
func (c *Client) Close() error { return c.c.Close() }

// EnqueuePing publishes a telemetry:ping task under a producer span and carries
// the W3C trace context in the task headers.
func (c *Client) EnqueuePing(ctx context.Context) error {
	ctx, span := otel.Tracer("ephematool/queue").Start(ctx, "telemetry:ping publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "asynq"),
			attribute.String("messaging.destination.name", "default"),
		))
	defer span.End()

	task := asynq.NewTaskWithHeaders(TypePing, nil, InjectHeaders(ctx))
	if _, err := c.c.EnqueueContext(ctx, task, asynq.MaxRetry(1)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

// EnqueueEnvDispatch publishes an env:dispatch task for envID on the dispatch queue. The W3C
// trace context rides in the task headers, so the worker's spans join the create request's trace.
func (c *Client) EnqueueEnvDispatch(ctx context.Context, envID string) error {
	ctx, span := otel.Tracer("ephematool/queue").Start(ctx, "env:dispatch publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "asynq"),
			attribute.String("messaging.destination.name", QueueDispatch),
		))
	defer span.End()

	payload, err := json.Marshal(EnvPayload{EnvID: envID})
	if err != nil {
		return err
	}
	task := asynq.NewTaskWithHeaders(TypeEnvDispatch, payload, InjectHeaders(ctx))
	if _, err := c.c.EnqueueContext(ctx, task, asynq.Queue(QueueDispatch), asynq.MaxRetry(3)); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
```
### 6.3 `internal/queue/client_test.go` (new)

`internal/queue/client_test.go`

```go
package queue

import (
	"encoding/json"
	"testing"

	"github.com/alicebob/miniredis/v2"
	"github.com/hibiken/asynq"
)

func TestEnqueueEnvDispatch(t *testing.T) {
	mr := miniredis.RunT(t)
	c := NewClient(mr.Addr())
	t.Cleanup(func() { _ = c.Close() })

	if err := c.EnqueueEnvDispatch(t.Context(), "pv-aaaaaaaaaa"); err != nil {
		t.Fatal(err)
	}

	insp := asynq.NewInspector(asynq.RedisClientOpt{Addr: mr.Addr()})
	t.Cleanup(func() { _ = insp.Close() })
	pending, err := insp.ListPendingTasks(QueueDispatch) // the dedicated queue, not "default"
	if err != nil || len(pending) != 1 {
		t.Fatalf("pending=%d err=%v", len(pending), err)
	}
	task := pending[0]
	var p EnvPayload
	if err := json.Unmarshal(task.Payload, &p); err != nil {
		t.Fatal(err)
	}
	if task.Type != TypeEnvDispatch || p.EnvID != "pv-aaaaaaaaaa" || task.MaxRetry != 3 {
		t.Fatalf("task=%+v payload=%+v", task, p)
	}
	if def, _ := insp.ListPendingTasks("default"); len(def) != 0 {
		t.Fatal("dispatch task leaked into the default queue")
	}
}
```
```bash
bazel run //:gazelle
bazel test //internal/queue/...
```

---

## 7. `internal/envs`: the create service (plan 02-10, task 1)

`Service.Create` is the heart of ENV-01/ENV-02. The **order of checks is part of the contract** and is tested:

| # | Check | Refusal | Audit outcome |
|---|---|---|---|
| 1 | role, resolved **now** (WR-01) | `403` `not_creator` | denied |
| 2 | repo in `ALLOWED_REPOS` (exact, case-insensitive; empty list allows nothing) | `422` `repo_not_allowed` | denied |
| 3 | local checks **collected together**: ref format, template, size, values schema | `422` first error's code | rejected |
| 4 | ref → commit SHA (fork PRs refused) | `422` `ref_not_found` / `pr_not_eligible`; outage `503` | rejected |
| 5 | container image exists | `422` `image_not_found` | rejected |
| 6 | one transaction: environment row + success audit event | `500` on a database error | success |
| 7 | enqueue `env:dispatch` | failure is **logged**, request still succeeds (the sweeper recovers it) | n/a |

Why this order: the allowlist runs before **any** GitHub call (no SSRF through the repo field); cheap local checks run before network calls; and on any refusal **nothing is dispatched**. The repo stored is the allowlist's spelling, so case variants cannot create duplicates.

Details that matter:

- The environment id is `pv-` plus 10 random characters from `[a-z0-9]`; it becomes the namespace and the host label and is **never** derived from a branch or any user text.
- The success audit row and the environment row share one transaction (`pgStore`), so neither can exist without the other.
- When the template is missing or unknown, values are still validated against the **default (first) template**, so every problem appears in one response.
- If the audit write for a refusal itself fails, the user still gets their refusal but `audited` is `false`, so the UI never claims "this attempt was recorded" when it was not.
- All OpenTelemetry code is in `telemetry.go`, so the rest of the package is plain Go. The metric carries only `outcome`, never an environment id.

### 7.1 `ids.go`, `errors.go`

`internal/envs/ids.go`

```go
package envs

import (
	"crypto/rand"
	"encoding/hex"
)

const idAlphabet = "abcdefghijklmnopqrstuvwxyz0123456789"

// NewID returns "pv-" plus 10 random characters from [a-z0-9]. The id becomes the Kubernetes
// namespace and the preview host label, so it is never derived from a branch or a user value.
func NewID() string {
	// 252 = 7*36: bytes at or above it are discarded so every character is equally likely.
	out := make([]byte, 0, 10)
	buf := make([]byte, 16)
	for len(out) < 10 {
		if _, err := rand.Read(buf); err != nil {
			panic("crypto/rand unavailable: " + err.Error()) // the process cannot run safely without it
		}
		for _, b := range buf {
			if b < 252 && len(out) < 10 {
				out = append(out, idAlphabet[int(b)%36])
			}
		}
	}
	return "pv-" + string(out)
}

// newCorrelationID is the id the workflow's run-name carries so a run can be found again when
// the dispatch response has no run id (D-15). It is separate from the environment id.
func newCorrelationID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return "cr-" + hex.EncodeToString(b)
}
```
`internal/envs/errors.go`

```go
package envs

import (
	"fmt"

	"github.com/ephematool/ephematool/internal/catalog"
)

// FieldError is one problem with one input, shown next to its field in the form.
type FieldError = catalog.FieldError

// Rejection is a refused create. It carries everything the HTTP layer needs to build the 403,
// 422 or 503 body. Create returns it as the error; any other error is a plain 5xx.
type Rejection struct {
	Status  int    // 403, 422 or 503
	Outcome string // audit outcome: denied or rejected
	Reason  string // stable machine code, also the audit reason
	Errors  []FieldError

	// 403 only: who was refused and which teams grant creator access (for the UI copy).
	CreatorTeams []string
	Login        string

	// Audited is true when the audit row was written. The UI says "recorded in the audit log" only then.
	Audited bool
}

func (r *Rejection) Error() string {
	return fmt.Sprintf("create refused: status=%d outcome=%s reason=%s", r.Status, r.Outcome, r.Reason)
}
```
### 7.2 `telemetry.go`

`internal/envs/telemetry.go`

```go
package envs

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"

	"github.com/ephematool/ephematool/internal/conventions"
)

// All OpenTelemetry use is in this file so the rest of the package stays plain Go.

var createdCounter metric.Int64Counter

func init() {
	// Attribute: outcome only (bounded). Never an environment id on a metric (cardinality).
	createdCounter, _ = otel.Meter("ephematool/envs").Int64Counter("ephematool.envs.created",
		metric.WithDescription("Create attempts by outcome"))
}

// startSpan opens the env.create span. The returned func ends it and, when the create
// succeeded, tags it with the environment id, owner and template from the convention constants.
func startSpan(ctx context.Context) (context.Context, func(envID, owner, template string, err error)) {
	ctx, span := otel.Tracer("ephematool/envs").Start(ctx, "env.create")
	return ctx, func(envID, owner, template string, err error) {
		if envID != "" {
			span.SetAttributes(
				attribute.String(conventions.AttrEnvID, envID),
				attribute.String(conventions.AttrOwner, owner),
				attribute.String(conventions.AttrTemplate, template),
			)
		}
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
		}
		span.End()
	}
}

// traceIdentity returns the W3C traceparent of ctx (stored on the row so the worker and the
// sweeper continue the SAME trace) and the bare trace id (shown in the UI and audit log).
func traceIdentity(ctx context.Context) (traceparent, traceID string) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		traceID = sc.TraceID().String()
	}
	return carrier["traceparent"], traceID
}

func countCreate(ctx context.Context, outcome string) {
	if createdCounter != nil {
		createdCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("outcome", outcome)))
	}
}
```
### 7.3 `pgstore.go`

`internal/envs/pgstore.go`

```go
package envs

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ephematool/ephematool/internal/audit"
	"github.com/ephematool/ephematool/internal/store"
)

// pgStore is the production Store: the environment row and its audit row commit in ONE
// transaction, so a success can never exist without its audit record (or the reverse).
type pgStore struct {
	pool *pgxpool.Pool
	q    *store.Queries
}

// NewPGStore returns the production Store backed by the runtime (app-role) pool.
func NewPGStore(pool *pgxpool.Pool, q *store.Queries) Store { return pgStore{pool: pool, q: q} }

func (s pgStore) CreateEnvironment(ctx context.Context, e NewEnvironment, ev audit.Event) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }() // no-op after a successful Commit
	qt := s.q.WithTx(tx)
	if _, err := qt.InsertEnvironment(ctx, store.InsertEnvironmentParams{
		ID: e.ID, OwnerGithubID: e.OwnerGithubID, OwnerLogin: e.OwnerLogin,
		Repo: e.Repo, RefType: e.RefType, Ref: e.Ref, Sha: e.SHA, Image: e.Image,
		Template: e.Template, ChartVersion: e.ChartVersion, Size: e.Size, Values: e.Values,
		CorrelationID: e.CorrelationID, Traceparent: e.Traceparent, TraceID: e.TraceID,
		ExpiresAt: pgtype.Timestamptz{Time: e.ExpiresAt, Valid: true},
	}); err != nil {
		var pg *pgconn.PgError
		if errors.As(err, &pg) && pg.Code == "23505" { // unique_violation on the primary key
			return ErrIDTaken
		}
		return err
	}
	ev.EnvironmentID = e.ID
	if err := audit.Record(ctx, qt, ev); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s pgStore) RecordAudit(ctx context.Context, ev audit.Event) error {
	return audit.Record(ctx, s.q, ev)
}
```
### 7.4 `service.go`

`internal/envs/service.go`

```go
// Package envs creates preview environments: it decides whether a request may proceed, validates
// every input on the server, records the attempt in the audit log and hands the work to the queue.
// It never talks to the workflow engine itself; the worker does that.
package envs

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/ephematool/ephematool/internal/audit"
	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/catalog"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/ghapp"
)

// RefResolver turns a branch, PR number or SHA into a full commit SHA. *ghapp.Client satisfies it.
type RefResolver interface {
	ResolveRef(ctx context.Context, repo string, t ghapp.RefType, ref string) (string, error)
}

// ImageChecker confirms the commit's image exists and returns its digest-pinned reference.
type ImageChecker interface {
	ImageRef(ctx context.Context, repo, sha string) (string, error)
}

// Enqueuer schedules the dispatch task. *queue.Client satisfies it.
type Enqueuer interface {
	EnqueueEnvDispatch(ctx context.Context, envID string) error
}

// Catalog is what the service needs from the template catalog. *catalog.Catalog satisfies it.
type Catalog interface {
	Templates() []catalog.Template
	Template(name string) (catalog.Template, bool)
	Size(name string) (catalog.Size, bool)
	ValidateValues(template string, values map[string]any) []catalog.FieldError
}

// NewEnvironment is the row to insert. It mirrors the table without importing sqlc types.
type NewEnvironment struct {
	ID            string
	OwnerGithubID int64
	OwnerLogin    string
	Repo, RefType string
	Ref, SHA      string
	Image         string
	Template      string
	ChartVersion  string
	Size          string
	Values        []byte // JSON object
	CorrelationID string
	Traceparent   string
	TraceID       string
	ExpiresAt     time.Time
}

// ErrIDTaken is returned by a Store when the generated id already exists (retry with a new one).
var ErrIDTaken = errors.New("environment id already exists")

// Store persists a create. Unit tests use a fake; production uses pgStore.
type Store interface {
	// CreateEnvironment inserts the row and the success audit event atomically.
	CreateEnvironment(ctx context.Context, e NewEnvironment, ev audit.Event) error
	// RecordAudit appends a denial or rejection.
	RecordAudit(ctx context.Context, ev audit.Event) error
}

// ServiceDeps are the collaborators of Service. Store is NewPGStore(pool, queries) in production.
type ServiceDeps struct {
	Cfg     config.Config
	Store   Store
	Roles   auth.Resolver
	Refs    RefResolver
	Images  ImageChecker
	Catalog Catalog
	Enqueue Enqueuer
	Now     func() time.Time
}

// Service implements environment creation.
type Service struct {
	cfg     config.Config
	store   Store
	roles   auth.Resolver
	refs    RefResolver
	images  ImageChecker
	catalog Catalog
	enqueue Enqueuer
	now     func() time.Time
}

// NewService builds a Service. A nil Refs or Images makes every create answer 503
// github_unavailable (audited), which is what an unconfigured GitHub App should look like.
func NewService(d ServiceDeps) *Service {
	now := d.Now
	if now == nil {
		now = time.Now
	}
	return &Service{cfg: d.Cfg, store: d.Store, roles: d.Roles, refs: d.Refs, images: d.Images,
		catalog: d.Catalog, enqueue: d.Enqueue, now: now}
}

// CreateRequest is the untrusted input of a create. Every field is validated here.
type CreateRequest struct {
	Repo, RefType, Ref, Template, Size string
	Values                             map[string]any
}

// Create runs the checks in a fixed order and returns the new environment id. The order is part
// of the contract (and tested): cheap local checks run before any network call, the repo
// allowlist runs before any GitHub request (no SSRF), and nothing is dispatched on any refusal.
//
//  1. role, resolved NOW (a creator removed from the team since login is refused)
//  2. repo allowlist
//  3. local checks, all collected together: ref format, template, size, values schema
//  4. resolve the ref to a commit SHA (fork PRs refused)
//  5. confirm the commit's container image exists
//  6. one transaction: insert the environment and the success audit event
//  7. enqueue the dispatch (a failure here is logged; the sweeper recovers it)
func (s *Service) Create(ctx context.Context, p auth.Principal, req CreateRequest) (string, error) {
	ctx, end := startSpan(ctx)
	id, err := s.create(ctx, p, req)
	end(id, p.Login, req.Template, err)
	return id, err
}

func (s *Service) create(ctx context.Context, p auth.Principal, req CreateRequest) (string, error) {
	if s.store == nil {
		return "", errors.New("envs: no store configured")
	}

	// 1. Role, resolved now. A lookup error resolves to viewer (fail closed) inside the resolver.
	role, err := s.roles.Resolve(ctx, p.Login)
	if err != nil {
		slog.WarnContext(ctx, "env.create.role_check_failed", "login", p.Login, "error", err)
	}
	if !auth.RoleAtLeast(role, auth.RoleCreator) {
		return "", s.refuse(ctx, p, &Rejection{
			Status: http.StatusForbidden, Outcome: audit.OutcomeDenied, Reason: audit.ReasonNotCreator,
			CreatorTeams: s.cfg.CreatorTeams, Login: p.Login,
		})
	}

	// 2. Allowlist: exact, case-insensitive, and an empty list allows nothing (fail closed).
	repo, ok := canonicalRepo(s.cfg.AllowedRepos, req.Repo)
	if !ok {
		return "", s.refuse(ctx, p, &Rejection{
			Status: http.StatusUnprocessableEntity, Outcome: audit.OutcomeDenied, Reason: audit.ReasonRepoNotAllowed,
			Errors: []FieldError{{Field: "repo", Code: audit.ReasonRepoNotAllowed, Message: "This repository is not allowed."}},
		})
	}

	// 3. Local checks, collected so the user fixes everything in one pass.
	if errs := s.localErrors(req); len(errs) > 0 {
		return "", s.refuse(ctx, p, &Rejection{
			Status: http.StatusUnprocessableEntity, Outcome: audit.OutcomeRejected, Reason: errs[0].Code, Errors: errs,
		})
	}
	tpl, _ := s.catalog.Template(req.Template) // known and usable: localErrors checked

	// 4. Ref -> SHA.
	if s.refs == nil || s.images == nil {
		return "", s.githubUnavailable(ctx, p, errors.New("GitHub App not configured"))
	}
	sha, err := s.refs.ResolveRef(ctx, repo, ghapp.RefType(req.RefType), req.Ref)
	if err != nil {
		return "", s.refError(ctx, p, err)
	}

	// 5. Image.
	image, err := s.images.ImageRef(ctx, repo, sha)
	if err != nil {
		if errors.Is(err, ghapp.ErrImageNotFound) {
			return "", s.refuse(ctx, p, &Rejection{
				Status: http.StatusUnprocessableEntity, Outcome: audit.OutcomeRejected, Reason: audit.ReasonImageNotFound,
				Errors: []FieldError{{Field: "ref", Code: audit.ReasonImageNotFound, Message: "No image was published for this commit."}},
			})
		}
		return "", s.githubUnavailable(ctx, p, err)
	}

	// 6. Insert + success audit in one transaction. Retry a primary-key collision with a new id.
	values := req.Values
	if values == nil {
		values = map[string]any{}
	}
	raw, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	traceparent, traceID := traceIdentity(ctx)
	row := NewEnvironment{
		OwnerGithubID: p.GithubID, OwnerLogin: p.Login, // owner is the signed-in user (D-08)
		Repo: repo, RefType: req.RefType, Ref: req.Ref, SHA: sha, Image: image,
		Template: tpl.Name, ChartVersion: tpl.Version, Size: req.Size, Values: raw,
		CorrelationID: newCorrelationID(), Traceparent: traceparent, TraceID: traceID,
		ExpiresAt: s.now().Add(s.cfg.EnvDefaultTTL), // informational in Phase 2 (D-13)
	}
	ev := audit.Event{
		ActorGithubID: p.GithubID, ActorLogin: p.Login, Action: audit.ActionEnvironmentCreate,
		Outcome: audit.OutcomeSuccess, TraceID: traceID,
	}
	for attempt := 0; ; attempt++ {
		row.ID = NewID()
		err = s.store.CreateEnvironment(ctx, row, ev)
		if !errors.Is(err, ErrIDTaken) || attempt >= 2 {
			break
		}
	}
	if err != nil {
		return "", err
	}
	countCreate(ctx, audit.OutcomeSuccess)
	slog.InfoContext(ctx, "env.created", "env_id", row.ID, "login", p.Login, "template", tpl.Name)

	// 7. Enqueue. The row already exists, so a failure here must not fail the request.
	if s.enqueue != nil {
		if err := s.enqueue.EnqueueEnvDispatch(ctx, row.ID); err != nil {
			slog.ErrorContext(ctx, "env.dispatch.enqueue_failed", "env_id", row.ID, "error", err)
		}
	}
	return row.ID, nil
}

// localErrors performs the checks that need no network.
func (s *Service) localErrors(req CreateRequest) []FieldError {
	var errs []FieldError
	switch ghapp.RefType(req.RefType) {
	case ghapp.RefBranch, ghapp.RefPR, ghapp.RefSHA:
		if err := ghapp.ValidateRefInput(ghapp.RefType(req.RefType), req.Ref); err != nil {
			errs = append(errs, FieldError{Field: "ref", Code: audit.ReasonRefInvalid, Message: "This is not a valid " + req.RefType + "."})
		}
	default:
		errs = append(errs, FieldError{Field: "refType", Code: audit.ReasonRefInvalid, Message: "Choose branch, PR or SHA."})
	}

	validTemplate := false
	switch t, ok := s.catalog.Template(req.Template); {
	case req.Template == "":
		errs = append(errs, FieldError{Field: "template", Code: audit.ReasonTemplateRequired, Message: "Select a template."})
	case !ok:
		errs = append(errs, FieldError{Field: "template", Code: audit.ReasonTemplateRequired, Message: "Select an approved template."})
	case !t.Form.Usable:
		errs = append(errs, FieldError{Field: "template", Code: audit.ReasonTemplateRequired, Message: "This template is not available in this form."})
	default:
		validTemplate = true
	}

	if _, ok := s.catalog.Size(req.Size); !ok {
		errs = append(errs, FieldError{Field: "size", Code: audit.ReasonSizeRequired, Message: "Select a size."})
	}

	// Values are validated against the chosen template, or the default (first) template when none
	// is chosen, so every problem still appears in the same response.
	target := req.Template
	if !validTemplate {
		target = s.defaultTemplate()
	}
	if target != "" {
		errs = append(errs, s.catalog.ValidateValues(target, req.Values)...)
	}
	return errs
}

func (s *Service) defaultTemplate() string {
	if ts := s.catalog.Templates(); len(ts) > 0 {
		return ts[0].Name
	}
	return ""
}

// refError maps a ref-resolution error to a rejection or a GitHub outage.
func (s *Service) refError(ctx context.Context, p auth.Principal, err error) error {
	reject := func(code, msg string) error {
		return s.refuse(ctx, p, &Rejection{
			Status: http.StatusUnprocessableEntity, Outcome: audit.OutcomeRejected, Reason: code,
			Errors: []FieldError{{Field: "ref", Code: code, Message: msg}},
		})
	}
	switch {
	case errors.Is(err, ghapp.ErrRefInvalid):
		return reject(audit.ReasonRefInvalid, "This is not a valid ref.")
	case errors.Is(err, ghapp.ErrRefNotFound):
		return reject(audit.ReasonRefNotFound, "This ref was not found in the repository.")
	case errors.Is(err, ghapp.ErrPRNotEligible):
		return reject(audit.ReasonPRNotEligible, "This pull request cannot be previewed (it is closed or from a fork).")
	default:
		return s.githubUnavailable(ctx, p, err)
	}
}

func (s *Service) githubUnavailable(ctx context.Context, p auth.Principal, cause error) error {
	slog.WarnContext(ctx, "env.create.github_unavailable", "login", p.Login, "error", cause)
	return s.refuse(ctx, p, &Rejection{
		Status: http.StatusServiceUnavailable, Outcome: audit.OutcomeRejected, Reason: audit.ReasonGitHubUnavailable,
	})
}

// refuse writes the one audit row for a refused attempt and returns the rejection. If the audit
// write itself fails the user still gets their refusal, but Audited stays false so the UI does
// not claim the attempt was recorded.
func (s *Service) refuse(ctx context.Context, p auth.Principal, rej *Rejection) error {
	ev := audit.Event{
		ActorGithubID: p.GithubID, ActorLogin: p.Login, Action: audit.ActionEnvironmentCreate,
		Outcome: rej.Outcome, Reason: rej.Reason,
	}
	if err := s.store.RecordAudit(ctx, ev); err != nil {
		slog.ErrorContext(ctx, "env.create.audit_failed", "outcome", rej.Outcome, "reason", rej.Reason, "error", err)
	} else {
		rej.Audited = true
	}
	countCreate(ctx, rej.Outcome)
	event := "env.create.rejected"
	if rej.Outcome == audit.OutcomeDenied {
		event = "env.create.denied"
	}
	slog.InfoContext(ctx, event, "login", p.Login, "reason", rej.Reason) // never the values
	return rej
}

// canonicalRepo returns the allowlist's spelling of repo, case-insensitively.
func canonicalRepo(allowed []string, repo string) (string, bool) {
	repo = strings.TrimSpace(repo)
	for _, a := range allowed {
		if strings.EqualFold(a, repo) {
			return a, true
		}
	}
	return "", false
}
```
### 7.5 Unit tests (no Docker)

They use hand-written fakes for the role resolver, GitHub, image check, queue and store, and assert the invariant of **every** refusal: exactly one audit row, no environment row, no enqueue.

`internal/envs/service_test.go`

```go
package envs

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ephematool/ephematool/internal/audit"
	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/catalog"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/ghapp"
)

// ---- fakes -----------------------------------------------------------------------------------

type fakeRoles map[string]string // login -> role

func (f fakeRoles) Resolve(_ context.Context, login string) (string, error) {
	if r, ok := f[login]; ok {
		return r, nil
	}
	return auth.RoleViewer, nil
}

type fakeRefs struct {
	sha   string
	err   error
	calls int
}

func (f *fakeRefs) ResolveRef(context.Context, string, ghapp.RefType, string) (string, error) {
	f.calls++
	return f.sha, f.err
}

type fakeImages struct {
	ref   string
	err   error
	calls int
}

func (f *fakeImages) ImageRef(context.Context, string, string) (string, error) {
	f.calls++
	return f.ref, f.err
}

type fakeEnq struct {
	mu  sync.Mutex
	ids []string
	err error
}

func (f *fakeEnq) EnqueueEnvDispatch(_ context.Context, id string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ids = append(f.ids, id)
	return f.err
}

type fakeStore struct {
	created []NewEnvironment
	audits  []audit.Event
	taken   int // how many CreateEnvironment calls fail with ErrIDTaken first
}

func (f *fakeStore) CreateEnvironment(_ context.Context, e NewEnvironment, ev audit.Event) error {
	if f.taken > 0 {
		f.taken--
		return ErrIDTaken
	}
	f.created = append(f.created, e)
	ev.EnvironmentID = e.ID
	f.audits = append(f.audits, ev)
	return nil
}

func (f *fakeStore) RecordAudit(_ context.Context, ev audit.Event) error {
	f.audits = append(f.audits, ev)
	return nil
}

// fakeCatalog: one usable template "web-app" 0.1.0 with replicas <= 3; sizes S, M, L.
type fakeCatalog struct{ unusable bool }

func (c fakeCatalog) Templates() []catalog.Template {
	return []catalog.Template{{Name: "web-app", Version: "0.1.0", Form: catalog.FormSchema{Usable: !c.unusable}}}
}

func (c fakeCatalog) Template(name string) (catalog.Template, bool) {
	if name != "web-app" {
		return catalog.Template{}, false
	}
	return c.Templates()[0], true
}

func (fakeCatalog) Size(name string) (catalog.Size, bool) {
	switch name {
	case "S", "M", "L":
		return catalog.Size{Name: name}, true
	}
	return catalog.Size{}, false
}

func (fakeCatalog) ValidateValues(_ string, v map[string]any) []catalog.FieldError {
	if r, ok := v["replicas"].(int); ok && r > 3 {
		return []catalog.FieldError{{Field: "values.replicas", Code: "schema_violation", Message: "maximum is 3"}}
	}
	return nil
}

// ---- harness ---------------------------------------------------------------------------------

type harness struct {
	svc    *Service
	store  *fakeStore
	refs   *fakeRefs
	images *fakeImages
	enq    *fakeEnq
}

var creator = auth.Principal{GithubID: 1007, Login: "octo-creator"}

func newHarness(t *testing.T) *harness {
	t.Helper()
	h := &harness{
		store:  &fakeStore{},
		refs:   &fakeRefs{sha: strings.Repeat("a", 40)},
		images: &fakeImages{ref: "ghcr.io/ephematool-e2e/demo-app@sha256:" + strings.Repeat("b", 64)},
		enq:    &fakeEnq{},
	}
	h.svc = NewService(ServiceDeps{
		Cfg: config.Config{
			AllowedRepos: []string{"ephematool-e2e/demo-app"}, CreatorTeams: []string{"ephematool-e2e/creators"},
			EnvDefaultTTL: 24 * time.Hour,
		},
		Store: h.store, Roles: fakeRoles{"octo-creator": auth.RoleCreator, "octo-admin": auth.RoleAdmin},
		Refs: h.refs, Images: h.images, Catalog: fakeCatalog{}, Enqueue: h.enq,
		Now: func() time.Time { return time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC) },
	})
	return h
}

func goodReq() CreateRequest {
	return CreateRequest{Repo: "ephematool-e2e/demo-app", RefType: "branch", Ref: "main", Template: "web-app", Size: "S"}
}

// mustReject runs Create and returns the Rejection.
func mustReject(t *testing.T, h *harness, p auth.Principal, req CreateRequest) *Rejection {
	t.Helper()
	id, err := h.svc.Create(t.Context(), p, req)
	var rej *Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("want *Rejection, got id=%q err=%v", id, err)
	}
	return rej
}

// assertNoDispatchAndOneAudit is the invariant of EVERY refusal.
func assertNoDispatchAndOneAudit(t *testing.T, h *harness, outcome, reason string) {
	t.Helper()
	if len(h.enq.ids) != 0 {
		t.Errorf("enqueued %v on a refused request", h.enq.ids)
	}
	if len(h.store.created) != 0 {
		t.Errorf("inserted %d environments on a refused request", len(h.store.created))
	}
	if len(h.store.audits) != 1 {
		t.Fatalf("audit events = %d, want exactly 1", len(h.store.audits))
	}
	if ev := h.store.audits[0]; ev.Outcome != outcome || ev.Reason != reason || ev.Action != audit.ActionEnvironmentCreate {
		t.Errorf("audit = %+v, want outcome=%s reason=%s", ev, outcome, reason)
	}
}

// ---- tests -----------------------------------------------------------------------------------

func TestSuccess(t *testing.T) {
	h := newHarness(t)
	id, err := h.svc.Create(t.Context(), creator, goodReq())
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^pv-[a-z0-9]{10}$`).MatchString(id) {
		t.Fatalf("id=%q", id)
	}
	if len(h.store.created) != 1 || len(h.store.audits) != 1 || h.store.audits[0].Outcome != audit.OutcomeSuccess {
		t.Fatalf("created=%d audits=%+v", len(h.store.created), h.store.audits)
	}
	row := h.store.created[0]
	if row.OwnerLogin != "octo-creator" || row.OwnerGithubID != 1007 || row.Template != "web-app" ||
		row.ChartVersion != "0.1.0" || row.SHA != strings.Repeat("a", 40) || !strings.HasPrefix(row.Image, "ghcr.io/") ||
		row.ExpiresAt != time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC) || string(row.Values) != "{}" ||
		!strings.HasPrefix(row.CorrelationID, "cr-") {
		t.Fatalf("row=%+v", row)
	}
	if len(h.enq.ids) != 1 || h.enq.ids[0] != id {
		t.Fatalf("enqueued=%v want [%s]", h.enq.ids, id)
	}
}

func TestAdminMayCreate(t *testing.T) {
	h := newHarness(t)
	if _, err := h.svc.Create(t.Context(), auth.Principal{GithubID: 1001, Login: "octo-admin"}, goodReq()); err != nil {
		t.Fatal(err)
	}
}

func TestViewerDenied(t *testing.T) {
	h := newHarness(t)
	rej := mustReject(t, h, auth.Principal{GithubID: 1002, Login: "octo-viewer"}, goodReq())
	if rej.Status != 403 || rej.Outcome != audit.OutcomeDenied || rej.Reason != audit.ReasonNotCreator ||
		rej.Login != "octo-viewer" || len(rej.CreatorTeams) != 1 || !rej.Audited {
		t.Fatalf("rej=%+v", rej)
	}
	assertNoDispatchAndOneAudit(t, h, audit.OutcomeDenied, audit.ReasonNotCreator)
	if h.refs.calls != 0 {
		t.Fatal("resolver called for a denied user")
	}
}

func TestRepoAllowlist(t *testing.T) {
	h := newHarness(t)
	req := goodReq()
	req.Repo = "Ephematool-E2E/Demo-App" // case-insensitive
	if _, err := h.svc.Create(t.Context(), creator, req); err != nil {
		t.Fatalf("case-insensitive match failed: %v", err)
	}
	if got := h.store.created[0].Repo; got != "ephematool-e2e/demo-app" {
		t.Fatalf("stored repo %q, want the allowlist spelling", got)
	}

	h = newHarness(t)
	req.Repo = "x/y"
	rej := mustReject(t, h, creator, req)
	if rej.Status != 422 || rej.Outcome != audit.OutcomeDenied || rej.Errors[0].Field != "repo" {
		t.Fatalf("rej=%+v", rej)
	}
	assertNoDispatchAndOneAudit(t, h, audit.OutcomeDenied, audit.ReasonRepoNotAllowed)
	if h.refs.calls != 0 {
		t.Fatal("GitHub called for a repo outside the allowlist (SSRF guard)")
	}
}

func TestEmptyAllowlistFailsClosed(t *testing.T) {
	h := newHarness(t)
	h.svc.cfg.AllowedRepos = nil
	rej := mustReject(t, h, creator, goodReq())
	if rej.Reason != audit.ReasonRepoNotAllowed || rej.Outcome != audit.OutcomeDenied {
		t.Fatalf("rej=%+v", rej)
	}
	assertNoDispatchAndOneAudit(t, h, audit.OutcomeDenied, audit.ReasonRepoNotAllowed)
}

func TestLocalErrorsAreCollectedTogether(t *testing.T) {
	h := newHarness(t)
	req := CreateRequest{Repo: "ephematool-e2e/demo-app", RefType: "branch", Ref: "../x", Template: "", Size: "XL",
		Values: map[string]any{"replicas": 9}}
	rej := mustReject(t, h, creator, req)
	var codes []string
	for _, e := range rej.Errors {
		codes = append(codes, e.Field+":"+e.Code)
	}
	want := []string{"ref:ref_invalid", "template:template_required", "size:size_required", "values.replicas:schema_violation"}
	if strings.Join(codes, ",") != strings.Join(want, ",") {
		t.Fatalf("errors=%v want %v", codes, want)
	}
	if rej.Outcome != audit.OutcomeRejected || rej.Reason != audit.ReasonRefInvalid || rej.Status != 422 {
		t.Fatalf("rej=%+v", rej)
	}
	assertNoDispatchAndOneAudit(t, h, audit.OutcomeRejected, audit.ReasonRefInvalid)
	if h.refs.calls != 0 || h.images.calls != 0 {
		t.Fatal("network called before local validation passed")
	}
}

func TestBadRefTypeAndUnusableTemplate(t *testing.T) {
	h := newHarness(t)
	req := goodReq()
	req.RefType = "tag"
	if rej := mustReject(t, h, creator, req); rej.Errors[0].Field != "refType" {
		t.Fatalf("rej=%+v", rej)
	}
	h = newHarness(t)
	h.svc.catalog = fakeCatalog{unusable: true}
	if rej := mustReject(t, h, creator, goodReq()); rej.Errors[0].Field != "template" {
		t.Fatalf("rej=%+v", rej)
	}
}

func TestResolverRejections(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		reason string
	}{
		{"ref not found", ghapp.ErrRefNotFound, audit.ReasonRefNotFound},
		{"fork or closed PR", ghapp.ErrPRNotEligible, audit.ReasonPRNotEligible},
		{"invalid", ghapp.ErrRefInvalid, audit.ReasonRefInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t)
			h.refs.err = tc.err
			rej := mustReject(t, h, creator, goodReq())
			if rej.Status != 422 || rej.Outcome != audit.OutcomeRejected || rej.Errors[0].Field != "ref" || rej.Errors[0].Code != tc.reason {
				t.Fatalf("rej=%+v", rej)
			}
			assertNoDispatchAndOneAudit(t, h, audit.OutcomeRejected, tc.reason)
		})
	}
}

func TestImageNotFound(t *testing.T) {
	h := newHarness(t)
	h.images.err = ghapp.ErrImageNotFound
	rej := mustReject(t, h, creator, goodReq())
	if rej.Status != 422 || rej.Errors[0].Field != "ref" || rej.Reason != audit.ReasonImageNotFound {
		t.Fatalf("rej=%+v", rej)
	}
	assertNoDispatchAndOneAudit(t, h, audit.OutcomeRejected, audit.ReasonImageNotFound)
}

func TestGitHubOutageIs503AndAudited(t *testing.T) {
	for name, set := range map[string]func(*harness){
		"resolver":     func(h *harness) { h.refs.err = errors.New("dial tcp: i/o timeout") },
		"image":        func(h *harness) { h.images.err = errors.New("EOF") },
		"unconfigured": func(h *harness) { h.svc.refs = nil },
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness(t)
			set(h)
			rej := mustReject(t, h, creator, goodReq())
			if rej.Status != 503 || rej.Reason != audit.ReasonGitHubUnavailable || !rej.Audited {
				t.Fatalf("rej=%+v", rej)
			}
			assertNoDispatchAndOneAudit(t, h, audit.OutcomeRejected, audit.ReasonGitHubUnavailable)
		})
	}
}

func TestEnqueueFailureStillReturnsID(t *testing.T) {
	h := newHarness(t)
	h.enq.err = errors.New("redis down")
	id, err := h.svc.Create(t.Context(), creator, goodReq())
	if err != nil || id == "" {
		t.Fatalf("id=%q err=%v (the row exists; the sweeper will dispatch)", id, err)
	}
}

func TestIDCollisionRetries(t *testing.T) {
	h := newHarness(t)
	h.store.taken = 2
	if _, err := h.svc.Create(t.Context(), creator, goodReq()); err != nil {
		t.Fatalf("two collisions should be retried: %v", err)
	}
	h = newHarness(t)
	h.store.taken = 3
	if _, err := h.svc.Create(t.Context(), creator, goodReq()); !errors.Is(err, ErrIDTaken) {
		t.Fatalf("err=%v, want ErrIDTaken after 3 attempts", err)
	}
}

func TestNewID(t *testing.T) {
	re := regexp.MustCompile(`^pv-[a-z0-9]{10}$`)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewID()
		if !re.MatchString(id) || seen[id] {
			t.Fatalf("bad or duplicate id %q", id)
		}
		seen[id] = true
	}
}
```
```bash
bazel run //:gazelle
bazel test //internal/envs/...
```

---

## 8. The HTTP operations (plan 02-10, task 2)

Four operations, all requiring a session:

| Operation | Purpose |
|---|---|
| `get-environment-options` | repos, ref types, templates with form fields, sizes, default size and TTL, creator teams |
| `create-environment` | `202 {id}`; `403`/`422`/`503` carry the structured `CreateError` body |
| `get-environment` | one environment; `previewUrl` is empty **until it is running** |
| `list-environments` | the caller's most recent (1 to 20, default 5), newest first |

Two design choices:

1. **The create body has no schema constraints beyond length caps.** A request Huma rejects never reaches the service and would never be audited, so validation (and its audit row) happens in `envs.Service`. Only malformed JSON or an oversize field produces Huma's own `422`, whose shape differs (`errors[].message/location`); the UI handles both.
2. **Error bodies are declared explicitly** (`Responses`) so `openapi-typescript` emits the `CreateError` type for 403, 422 and 503.

### 8.1 `internal/httpapi/environments.go`

`internal/httpapi/environments.go`

```go
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/catalog"
	"github.com/ephematool/ephematool/internal/envs"
	"github.com/ephematool/ephematool/internal/previewauth"
	"github.com/ephematool/ephematool/internal/store"
)

// EnvCreator is what the create operation needs. *envs.Service satisfies it; tests use a stub.
type EnvCreator interface {
	Create(ctx context.Context, p auth.Principal, req envs.CreateRequest) (string, error)
}

// ---- request and response bodies -------------------------------------------------------------

// CreateInput is the POST /api/environments body. Validation is deliberately NOT done with
// schema constraints here: a request that Huma rejects never reaches the service, so it would
// never be audited. Only a size cap is declared; everything else is checked (and audited) by
// envs.Service.
type CreateInput struct {
	Body struct {
		Repo     string         `json:"repo" maxLength:"200" doc:"org/name of an allowed repository"`
		RefType  string         `json:"refType" maxLength:"10" doc:"branch, pr or sha"`
		Ref      string         `json:"ref" maxLength:"200"`
		Template string         `json:"template" maxLength:"100"`
		Size     string         `json:"size" maxLength:"10" doc:"S, M or L"`
		Values   map[string]any `json:"values,omitempty" doc:"Template options; validated against the template's schema"`
	}
}

// CreateOutput is the 202 body.
type CreateOutput struct {
	Body struct {
		ID string `json:"id"`
	}
}

// FieldErrorBody is one field-level problem (see catalog.FieldError).
type FieldErrorBody struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
}

// CreateError is the body of the 403, 422 and 503 answers of create-environment. It implements
// huma.StatusError so Huma writes it as the response body unchanged.
type CreateError struct {
	Status       int              `json:"status"`
	Title        string           `json:"title"`
	Code         string           `json:"code,omitempty"`
	Login        string           `json:"login,omitempty"`
	CreatorTeams []string         `json:"creatorTeams,omitempty"`
	Errors       []FieldErrorBody `json:"errors,omitempty"`
	Audited      bool             `json:"audited"`
}

func (e *CreateError) Error() string  { return e.Title }
func (e *CreateError) GetStatus() int { return e.Status }

func toCreateError(r *envs.Rejection) *CreateError {
	out := &CreateError{Status: r.Status, Audited: r.Audited, Code: r.Reason}
	switch r.Status {
	case http.StatusForbidden:
		out.Title, out.Login, out.CreatorTeams = "not allowed to create environments", r.Login, nonNilStrings(r.CreatorTeams)
	case http.StatusServiceUnavailable:
		out.Title = "GitHub is unavailable; try again shortly"
	default:
		out.Title = "request rejected"
	}
	for _, e := range r.Errors {
		out.Errors = append(out.Errors, FieldErrorBody{Field: e.Field, Code: e.Code, Message: e.Message, Path: e.Path})
	}
	return out
}

func nonNilStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return s
}

// OptionsBody is everything the create form needs to render.
type OptionsBody struct {
	Repos             []string         `json:"repos"`
	RefTypes          []string         `json:"refTypes"`
	Templates         []TemplateOption `json:"templates"`
	Sizes             []SizeOption     `json:"sizes"`
	DefaultSize       string           `json:"defaultSize"`
	DefaultTTLSeconds int64            `json:"defaultTtlSeconds"`
	CreatorTeams      []string         `json:"creatorTeams"`
}

// TemplateOption is one approved template and the form fields trimmed from its schema.
type TemplateOption struct {
	Name        string              `json:"name"`
	Version     string              `json:"version"`
	Description string              `json:"description"`
	Usable      bool                `json:"usable"`
	OmittedSome bool                `json:"omittedSome"`
	Fields      []catalog.FormField `json:"fields"`
}

// SizeOption is one size preset with its display text.
type SizeOption struct {
	Name   string `json:"name"`
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
	Helper string `json:"helper"`
}

// OptionsResponse wraps OptionsBody for Huma.
type OptionsResponse struct{ Body OptionsBody }

// OwnerBody is the creator of an environment.
type OwnerBody struct {
	Login     string `json:"login"`
	GithubID  int64  `json:"githubId"`
	AvatarURL string `json:"avatarUrl"`
}

// EnvSize is the chosen preset with display values.
type EnvSize struct {
	Name   string `json:"name"`
	CPU    string `json:"cpu"`
	Memory string `json:"memory"`
}

// EnvironmentBody is one environment as the detail page shows it.
type EnvironmentBody struct {
	ID              string     `json:"id"`
	Status          string     `json:"status" enum:"requested,provisioning,running,failed"`
	FailureReason   string     `json:"failureReason"`
	Owner           OwnerBody  `json:"owner"`
	Repo            string     `json:"repo"`
	RefType         string     `json:"refType"`
	Ref             string     `json:"ref"`
	SHA             string     `json:"sha"`
	Template        string     `json:"template"`
	ChartVersion    string     `json:"chartVersion"`
	Size            EnvSize    `json:"size"`
	CreatedAt       time.Time  `json:"createdAt"`
	ExpiresAt       time.Time  `json:"expiresAt"`
	RunningAt       *time.Time `json:"runningAt,omitempty"`
	RunHTMLURL      string     `json:"runHtmlUrl"`
	TraceID         string     `json:"traceId"`
	GrafanaTraceURL string     `json:"grafanaTraceUrl"`
	PreviewURL      string     `json:"previewUrl"` // empty until the environment is running
}

// EnvironmentResponse wraps EnvironmentBody for Huma.
type EnvironmentResponse struct{ Body EnvironmentBody }

// GetEnvironmentInput is the path parameter of get-environment.
type GetEnvironmentInput struct {
	ID string `path:"id" pattern:"^pv-[a-z0-9]{10}$"`
}

// ListInput is the query of list-environments. The bounds stop an unbounded read.
type ListInput struct {
	Limit int `query:"limit" minimum:"1" maximum:"20" default:"5"`
}

// EnvironmentSummary is one row of the launch page's "recent environments" list.
type EnvironmentSummary struct {
	ID        string    `json:"id"`
	Status    string    `json:"status" enum:"requested,provisioning,running,failed"`
	Template  string    `json:"template"`
	CreatedAt time.Time `json:"createdAt"`
}

// ListResponse wraps the list for Huma.
type ListResponse struct {
	Body struct {
		Environments []EnvironmentSummary `json:"environments"`
	}
}

// ---- registration ----------------------------------------------------------------------------

// registerEnvironments adds the four environment operations. It must be nil-safe at
// registration time (OpenAPI generation and the security walk pass empty Deps); only the handler
// bodies may use deps.
func registerEnvironments(api huma.API, deps Deps) {
	sec := []map[string][]string{{"session": {}}}

	huma.Register(api, huma.Operation{
		OperationID: "get-environment-options", Method: http.MethodGet, Path: "/api/environments/options",
		Summary: "Repositories, templates, sizes and defaults for the create form", Security: sec,
	}, func(ctx context.Context, _ *struct{}) (*OptionsResponse, error) { return optionsHandler(deps) })

	// Declare the error bodies explicitly so openapi-typescript emits the CreateError type.
	errSchema := huma.SchemaFromType(api.OpenAPI().Components.Schemas, reflect.TypeOf(CreateError{}))
	errResp := func(desc string) *huma.Response {
		return &huma.Response{Description: desc, Content: map[string]*huma.MediaType{"application/json": {Schema: errSchema}}}
	}
	huma.Register(api, huma.Operation{
		OperationID: "create-environment", Method: http.MethodPost, Path: "/api/environments",
		Summary: "Request a new preview environment", DefaultStatus: http.StatusAccepted, Security: sec,
		Responses: map[string]*huma.Response{
			"403": errResp("Not a creator (checked at request time)"),
			"422": errResp("One or more fields were rejected"),
			"503": errResp("GitHub is unavailable"),
		},
	}, func(ctx context.Context, in *CreateInput) (*CreateOutput, error) { return createHandler(ctx, deps, in) })

	huma.Register(api, huma.Operation{
		OperationID: "get-environment", Method: http.MethodGet, Path: "/api/environments/{id}",
		Summary: "One environment", Security: sec,
	}, func(ctx context.Context, in *GetEnvironmentInput) (*EnvironmentResponse, error) {
		return getHandler(ctx, deps, in.ID)
	})

	huma.Register(api, huma.Operation{
		OperationID: "list-environments", Method: http.MethodGet, Path: "/api/environments",
		Summary: "The caller's most recent environments", Security: sec,
	}, func(ctx context.Context, in *ListInput) (*ListResponse, error) {
		return listHandler(ctx, deps, in.Limit)
	})
}

// ---- handlers --------------------------------------------------------------------------------

func optionsHandler(deps Deps) (*OptionsResponse, error) {
	if deps.Catalog == nil {
		return nil, huma.Error503ServiceUnavailable("templates are not loaded")
	}
	b := OptionsBody{
		Repos: nonNilStrings(deps.Config.AllowedRepos), RefTypes: []string{"branch", "pr", "sha"},
		DefaultSize: "S", DefaultTTLSeconds: int64(deps.Config.EnvDefaultTTL.Seconds()),
		CreatorTeams: nonNilStrings(deps.Config.CreatorTeams),
	}
	for _, t := range deps.Catalog.Templates() {
		fields := t.Form.Fields
		if fields == nil {
			fields = []catalog.FormField{}
		}
		b.Templates = append(b.Templates, TemplateOption{
			Name: t.Name, Version: t.Version, Description: t.Description,
			Usable: t.Form.Usable, OmittedSome: t.Form.OmittedSome, Fields: fields,
		})
	}
	for _, s := range deps.Catalog.Sizes() {
		b.Sizes = append(b.Sizes, SizeOption{Name: s.Name, CPU: s.DisplayCPU, Memory: s.DisplayMemory, Helper: s.Helper})
	}
	return &OptionsResponse{Body: b}, nil
}

func createHandler(ctx context.Context, deps Deps, in *CreateInput) (*CreateOutput, error) {
	if deps.Envs == nil {
		return nil, huma.Error503ServiceUnavailable("environments are unavailable")
	}
	p, ok := auth.PrincipalFrom(ctx, deps.Sessions)
	if !ok {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	id, err := deps.Envs.Create(ctx, p, envs.CreateRequest{
		Repo: in.Body.Repo, RefType: in.Body.RefType, Ref: in.Body.Ref,
		Template: in.Body.Template, Size: in.Body.Size, Values: in.Body.Values,
	})
	var rej *envs.Rejection
	if errors.As(err, &rej) {
		return nil, toCreateError(rej)
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("could not create the environment")
	}
	out := &CreateOutput{}
	out.Body.ID = id
	return out, nil
}

func getHandler(ctx context.Context, deps Deps, id string) (*EnvironmentResponse, error) {
	if deps.Queries == nil {
		return nil, huma.Error503ServiceUnavailable("database is unavailable")
	}
	e, err := deps.Queries.GetEnvironment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, huma.Error404NotFound("environment not found")
	}
	if err != nil {
		return nil, huma.Error500InternalServerError("could not read the environment")
	}
	return &EnvironmentResponse{Body: environmentBody(ctx, deps, e)}, nil
}

func listHandler(ctx context.Context, deps Deps, limit int) (*ListResponse, error) {
	if deps.Queries == nil {
		return nil, huma.Error503ServiceUnavailable("database is unavailable")
	}
	p, ok := auth.PrincipalFrom(ctx, deps.Sessions)
	if !ok {
		return nil, huma.Error401Unauthorized("authentication required")
	}
	rows, err := deps.Queries.ListEnvironmentsByOwner(ctx, store.ListEnvironmentsByOwnerParams{
		OwnerGithubID: p.GithubID, Limit: int32(limit),
	})
	if err != nil {
		return nil, huma.Error500InternalServerError("could not list environments")
	}
	out := &ListResponse{}
	out.Body.Environments = []EnvironmentSummary{}
	for _, r := range rows {
		out.Body.Environments = append(out.Body.Environments, EnvironmentSummary{
			ID: r.ID, Status: r.Status, Template: r.Template, CreatedAt: r.CreatedAt.Time,
		})
	}
	return out, nil
}

func environmentBody(ctx context.Context, deps Deps, e store.Environment) EnvironmentBody {
	b := EnvironmentBody{
		ID: e.ID, Status: e.Status, FailureReason: e.FailureReason,
		Owner: OwnerBody{Login: e.OwnerLogin, GithubID: e.OwnerGithubID},
		Repo:  e.Repo, RefType: e.RefType, Ref: e.Ref, SHA: e.Sha,
		Template: e.Template, ChartVersion: e.ChartVersion, Size: EnvSize{Name: e.Size},
		CreatedAt: e.CreatedAt.Time, ExpiresAt: e.ExpiresAt.Time,
		RunHTMLURL: e.RunHtmlUrl, TraceID: e.TraceID,
		GrafanaTraceURL: grafanaTraceURL(deps.Config.GrafanaPublicURL, e.TraceID),
	}
	if e.RunningAt.Valid {
		t := e.RunningAt.Time
		b.RunningAt = &t
	}
	if e.Status == "running" { // the URL is shown only when the preview actually answered its probe
		b.PreviewURL = deps.Config.PreviewURL(e.ID)
	}
	if deps.Catalog != nil {
		if s, ok := deps.Catalog.Size(e.Size); ok {
			b.Size.CPU, b.Size.Memory = s.DisplayCPU, s.DisplayMemory
		}
	}
	if deps.Queries != nil { // avatar lives on the users row, not the environment
		if u, err := deps.Queries.GetUserByGithubID(ctx, e.OwnerGithubID); err == nil {
			b.Owner.AvatarURL = u.AvatarUrl
		}
	}
	return b
}

// grafanaTraceURL builds a Grafana Explore link that opens the trace in Tempo.
func grafanaTraceURL(base, traceID string) string {
	if traceID == "" || base == "" {
		return ""
	}
	panes := fmt.Sprintf(`{"a":{"datasource":"tempo","queries":[{"refId":"A","datasource":{"type":"tempo","uid":"tempo"},`+
		`"queryType":"traceql","query":%q}],"range":{"from":"now-24h","to":"now"}}}`, traceID)
	return strings.TrimRight(base, "/") + "/explore?schemaVersion=1&orgId=1&panes=" + url.QueryEscape(panes)
}

// NewEnvLookup adapts the store to previewauth.EnvLookup.
func NewEnvLookup(q *store.Queries) previewauth.EnvLookup { return storeEnvLookup{q: q} }

type storeEnvLookup struct{ q *store.Queries }

func (s storeEnvLookup) EnvironmentExists(ctx context.Context, id string) (bool, error) {
	if s.q == nil {
		return false, errors.New("no database")
	}
	_, err := s.q.GetEnvironment(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return err == nil, err
}
```
### 8.2 `internal/httpapi/api.go` (replace)

`Deps` gains `Envs`, `Catalog`, `Preview` and `PreviewAuth`; `New` registers the environment operations and the preview routes. All are optional so `httpapi.New(httpapi.Deps{})` still builds for OpenAPI generation. The ForwardAuth handler is a **plain mux route outside `/api/`**, wrapped with `RouteHandler` so `http.route` stays bounded; nginx never proxies it and it is not in the OpenAPI document.

`internal/httpapi/api.go`

```go
// Package httpapi wires the Huma API, route-accurate telemetry and handlers.
package httpapi

import (
	"context"
	"net/http"

	"github.com/alexedwards/scs/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/catalog"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/previewauth"
	"github.com/ephematool/ephematool/internal/queue"
	"github.com/ephematool/ephematool/internal/status"
	"github.com/ephematool/ephematool/internal/store"
)

// Enqueuer publishes the telemetry ping task. *queue.Client satisfies it.
type Enqueuer interface {
	EnqueuePing(ctx context.Context) error
}

// Deps are the collaborators handlers need. Later plans add fields.
type Deps struct {
	Config config.Config
	Redis  *redis.Client
	Queue  *queue.Client
	// Enqueuer overrides Queue when set (tests).
	Enqueuer Enqueuer
	// Sessions, Queries and Auth are optional; Auth is built from the others
	// when nil. All may be nil when only the OpenAPI spec is generated.
	Sessions *scs.SessionManager
	Queries  *store.Queries
	Auth     *auth.Handlers
	// Status is the background prober whose cached snapshot backs /api/status.
	Status *status.Prober

	// Phase 2. All optional: the OpenAPI generator and the security test pass none of them.
	Envs    EnvCreator       // create-environment (envs.Service)
	Catalog *catalog.Catalog // options, size display values
	// Preview serves POST /api/preview/ticket; PreviewAuth is the ForwardAuth handler Traefik calls.
	Preview     *previewauth.Handlers
	PreviewAuth http.Handler
}

func (d Deps) enqueuer() Enqueuer {
	if d.Enqueuer != nil {
		return d.Enqueuer
	}
	if d.Queue != nil {
		return d.Queue
	}
	return nil
}

// New builds the HTTP handler and the Huma API. Layer order, outermost first:
// CrossOriginProtection -> otelhttp -> TraceIDHeader -> sessions.LoadAndSave -> mux.
func New(deps Deps) (http.Handler, huma.API) {
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("Ephematool API", buildinfo.Version))
	api.UseMiddleware(RouteTagging)
	if deps.Auth == nil {
		deps.Auth = auth.NewHandlers(deps.Config, deps.Sessions, deps.Queries)
	}
	auth.Register(api, mux, deps.Auth, RouteHandler)
	registerStatus(api, deps)
	registerEnvironments(api, deps)
	previewauth.Register(api, mux, deps.Preview, deps.PreviewAuth, RouteHandler)

	var inner http.Handler = mux
	if deps.Sessions != nil {
		inner = deps.Sessions.LoadAndSave(mux)
	}
	h := otelhttp.NewHandler(TraceIDHeader(inner), "ephematool-api")
	return http.NewCrossOriginProtection().Handler(h), api
}
```
### 8.3 Tests

`internal/httpapi/environments_test.go`

```go
package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/redis/go-redis/v9"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/catalog"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/envs"
	"github.com/ephematool/ephematool/internal/store"
	"github.com/ephematool/ephematool/templates"
)

type stubCreator struct {
	id  string
	err error
	got envs.CreateRequest
	who auth.Principal
}

func (s *stubCreator) Create(_ context.Context, p auth.Principal, r envs.CreateRequest) (string, error) {
	s.got, s.who = r, p
	return s.id, s.err
}

type envHarness struct {
	h      http.Handler
	cookie *http.Cookie
}

func newEnvHarness(t *testing.T, creator EnvCreator) *envHarness {
	t.Helper()
	cat, err := catalog.Load(templates.FS)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		AllowedRepos: []string{"ephematool-e2e/demo-app"}, CreatorTeams: []string{"ephematool-e2e/creators"},
		EnvDefaultTTL: 24 * time.Hour, SessionIdleTimeout: time.Hour, SessionLifetime: 2 * time.Hour,
		PreviewScheme: "http", PreviewDomain: "preview.localhost", PreviewPort: 8088,
	}
	mr := miniredis.RunT(t)
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() { _ = rdb.Close() })
	sm := auth.NewSessionManager(rdb, cfg)

	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "0"))
	api.UseMiddleware(auth.RequireSession(api, sm))
	mux.HandleFunc("POST /test/login", func(w http.ResponseWriter, r *http.Request) {
		_ = sm.RenewToken(r.Context())
		sm.Put(r.Context(), auth.KeyUserID, int64(1007))
		sm.Put(r.Context(), auth.KeyLogin, "octo-creator")
	})
	registerEnvironments(api, Deps{Config: cfg, Sessions: sm, Envs: creator, Catalog: cat})

	hh := sm.LoadAndSave(mux)
	rec := httptest.NewRecorder()
	hh.ServeHTTP(rec, httptest.NewRequest("POST", "/test/login", nil))
	var c *http.Cookie
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == auth.CookieName {
			c = ck
		}
	}
	if c == nil {
		t.Fatal("no session cookie")
	}
	return &envHarness{h: hh, cookie: c}
}

func (e *envHarness) do(method, path, body string, session bool) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if session {
		req.AddCookie(e.cookie)
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

const createBody = `{"repo":"ephematool-e2e/demo-app","refType":"branch","ref":"main","template":"web-app","size":"S","values":{"replicas":2}}`

func TestCreateAccepted(t *testing.T) {
	sc := &stubCreator{id: "pv-aaaaaaaaaa"}
	h := newEnvHarness(t, sc)
	rec := h.do("POST", "/api/environments", createBody, true)
	if rec.Code != 202 || !strings.Contains(rec.Body.String(), `"id":"pv-aaaaaaaaaa"`) {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if sc.who.Login != "octo-creator" || sc.got.Repo != "ephematool-e2e/demo-app" || sc.got.Size != "S" {
		t.Fatalf("service saw who=%+v req=%+v", sc.who, sc.got)
	}
}

func TestCreateRejectionBodies(t *testing.T) {
	tests := []struct {
		name string
		rej  *envs.Rejection
		code int
		want []string
	}{
		{"422", &envs.Rejection{Status: 422, Reason: "ref_invalid", Audited: true,
			Errors: []envs.FieldError{{Field: "ref", Code: "ref_invalid", Message: "bad"}}},
			422, []string{`"errors":[{"field":"ref","code":"ref_invalid","message":"bad"}]`, `"audited":true`}},
		{"403", &envs.Rejection{Status: 403, Reason: "not_creator", Login: "octo-viewer", CreatorTeams: []string{"o/c"}, Audited: true},
			403, []string{`"code":"not_creator"`, `"login":"octo-viewer"`, `"creatorTeams":["o/c"]`, `"audited":true`}},
		{"503", &envs.Rejection{Status: 503, Reason: "github_unavailable", Audited: false},
			503, []string{`"audited":false`}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newEnvHarness(t, &stubCreator{err: tc.rej})
			rec := h.do("POST", "/api/environments", createBody, true)
			if rec.Code != tc.code {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
			}
			for _, w := range tc.want {
				if !strings.Contains(rec.Body.String(), w) {
					t.Errorf("body %s lacks %s", rec.Body, w)
				}
			}
		})
	}
}

func TestCreateRequiresSession(t *testing.T) {
	h := newEnvHarness(t, &stubCreator{id: "pv-aaaaaaaaaa"})
	if got := h.do("POST", "/api/environments", createBody, false).Code; got != 401 {
		t.Fatalf("code=%d", got)
	}
}

func TestOptions(t *testing.T) {
	h := newEnvHarness(t, &stubCreator{})
	rec := h.do("GET", "/api/environments/options", "", true)
	if rec.Code != 200 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	var b OptionsBody
	if err := json.Unmarshal(rec.Body.Bytes(), &b); err != nil {
		t.Fatal(err)
	}
	if len(b.Repos) != 1 || b.Repos[0] != "ephematool-e2e/demo-app" || b.DefaultSize != "S" || b.DefaultTTLSeconds != 86400 ||
		len(b.CreatorTeams) != 1 || len(b.RefTypes) != 3 {
		t.Fatalf("options=%+v", b)
	}
	if len(b.Templates) != 1 || b.Templates[0].Name != "web-app" || !b.Templates[0].Usable || len(b.Templates[0].Fields) != 4 {
		t.Fatalf("templates=%+v", b.Templates)
	}
	if len(b.Sizes) != 3 || b.Sizes[0].Name != "S" || b.Sizes[0].CPU != "0.5 vCPU" || b.Sizes[2].Memory != "2 GiB" {
		t.Fatalf("sizes=%+v", b.Sizes)
	}
}

func TestEnvironmentBodyPreviewURLOnlyWhenRunning(t *testing.T) {
	cat, _ := catalog.Load(templates.FS)
	deps := Deps{Catalog: cat, Config: config.Config{
		PreviewScheme: "http", PreviewDomain: "preview.localhost", PreviewPort: 8088, GrafanaPublicURL: "http://localhost:3000",
	}}
	row := store.Environment{
		ID: "pv-aaaaaaaaaa", Status: "provisioning", Size: "M", TraceID: "0af7651916cd43dd8448eb211c80319c",
		CreatedAt: pgtype.Timestamptz{Time: time.Now(), Valid: true}, ExpiresAt: pgtype.Timestamptz{Time: time.Now(), Valid: true},
	}
	b := environmentBody(t.Context(), deps, row)
	if b.PreviewURL != "" || b.Size.CPU != "1 vCPU" || b.Size.Memory != "1 GiB" {
		t.Fatalf("provisioning body=%+v", b)
	}
	if !strings.HasPrefix(b.GrafanaTraceURL, "http://localhost:3000/explore?") || !strings.Contains(b.GrafanaTraceURL, "0af7651916cd43dd8448eb211c80319c") {
		t.Fatalf("grafana url=%q", b.GrafanaTraceURL)
	}
	row.Status = "running"
	row.RunningAt = pgtype.Timestamptz{Time: time.Now(), Valid: true}
	b = environmentBody(t.Context(), deps, row)
	if b.PreviewURL != "http://pv-aaaaaaaaaa.preview.localhost:8088" || b.RunningAt == nil {
		t.Fatalf("running body=%+v", b)
	}
}

func TestGrafanaTraceURLEmptyWithoutTrace(t *testing.T) {
	if got := grafanaTraceURL("http://x", ""); got != "" {
		t.Fatalf("got %q", got)
	}
}
```
```bash
bazel run //:gazelle
bazel test //internal/httpapi/...        # includes the secure-by-default walk from 2a
```

`TestEveryOperationIsProtectedOrPublic` (2a) now covers the four new operations plus `post-preview-ticket`: all carry `Security: session`, so the set of public operations is still exactly `get-auth-config` and `post-auth-logout`.

---

## 9. Wire it up: `cmd/api/main.go` (replace)

New in `serve()`: the catalog is loaded from `templates.FS` (a broken chart stops startup); the create service gets `Refs`/`Images` only when the GitHub App exists (a typed-nil in an interface would otherwise pass the `== nil` check); preview keys that fail to load are a **warning, not a crash**, so previews fail closed (ForwardAuth `503`, ticket `403`) while the rest of the API keeps working.

`cmd/api/main.go`

```go
package main

//go:generate sh -c "go run . openapi > ../../api/openapi.json"

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/buildinfo"
	"github.com/ephematool/ephematool/internal/catalog"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/envs"
	"github.com/ephematool/ephematool/internal/ghapp"
	"github.com/ephematool/ephematool/internal/httpapi"
	"github.com/ephematool/ephematool/internal/previewauth"
	"github.com/ephematool/ephematool/internal/queue"
	"github.com/ephematool/ephematool/internal/status"
	"github.com/ephematool/ephematool/internal/store"
	"github.com/ephematool/ephematool/internal/telemetry"
	"github.com/ephematool/ephematool/templates"
)

func main() {
	cmd := "serve"
	if len(os.Args) > 1 {
		cmd = os.Args[1]
	}
	switch cmd {
	case "serve":
		if err := serve(); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
	case "migrate":
		if err := migrate(); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
	case "openapi":
		if err := printOpenAPI(); err != nil {
			fmt.Fprintln(os.Stderr, "fatal:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (serve|migrate|openapi)\n", cmd)
		os.Exit(2)
	}
}

func printOpenAPI() error {
	_, api := httpapi.New(httpapi.Deps{})
	b, err := api.OpenAPI().MarshalJSON()
	if err != nil {
		return err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, b, "", "  "); err != nil {
		return err
	}
	_, err = fmt.Fprintln(os.Stdout, buf.String())
	return err
}

func migrate() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	return runMigrations(ctx, cfg)
}

// runMigrations applies migrations with the OWNER connection (D-24). The API's
// runtime pool uses the restricted ephematool_app role and can never run DDL.
func runMigrations(ctx context.Context, cfg config.Config) error {
	pool, err := store.OpenUntraced(ctx, cfg.MigrationURL())
	if err != nil {
		return fmt.Errorf("open migration connection: %w", err)
	}
	defer pool.Close()
	version, err := store.MigrateVersion(ctx, pool)
	if err != nil {
		return fmt.Errorf("migrate: %w", err)
	}
	slog.InfoContext(ctx, "db.migrated", "version", version) // never log the URL
	return nil
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	shutdownTelemetry, err := telemetry.Setup(ctx, telemetry.Config{
		ServiceName: "ephematool-api",
		Version:     buildinfo.Version,
		Env:         cfg.AppEnv,
	})
	if err != nil {
		return err
	}
	defer func() {
		fctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = shutdownTelemetry(fctx)
	}()

	rdb := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})
	// Do not record Redis command text: scs session keys embed the session token
	// and the SET value is the serialized session, so db.statement would leak both.
	if err := redisotel.InstrumentTracing(rdb, redisotel.WithDBStatement(false)); err != nil {
		return err
	}
	if err := redisotel.InstrumentMetrics(rdb); err != nil {
		return err
	}
	defer rdb.Close()

	// Migrate first (owner role), then open the runtime pool (app role).
	if cfg.MigrateOnStart {
		if err := runMigrations(ctx, cfg); err != nil {
			return err
		}
	}
	pool, err := store.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer pool.Close()
	queries := store.New(pool)
	sessions := auth.NewSessionManager(rdb, cfg)

	// GitHub App client: optional. Without it creator teams cannot be checked
	// and (from 2b) creating environments is disabled with a clear message.
	gh, err := ghapp.New(cfg)
	switch {
	case errors.Is(err, ghapp.ErrNotConfigured):
		slog.WarnContext(ctx, "ghapp.not_configured", "hint", "set GITHUB_APP_ID, GITHUB_APP_INSTALLATION_ID, GITHUB_APP_PRIVATE_KEY_FILE")
		gh = nil
	case err != nil:
		return err
	}
	authHandlers := auth.NewHandlers(cfg, sessions, queries)
	var roles auth.Resolver = auth.NewRoleResolver(cfg, nil) // no App: team creators fail closed
	if gh != nil {                                           // avoid a typed-nil in the TeamChecker interface
		rr := auth.NewRoleResolver(cfg, gh)
		authHandlers.Roles, roles = rr, rr
	}

	q := queue.NewClient(cfg.RedisAddr)
	defer q.Close()

	// Approved templates are embedded in the binary; a broken chart or schema stops startup.
	cat, err := catalog.Load(templates.FS)
	if err != nil {
		return fmt.Errorf("load template catalog: %w", err)
	}

	// The create service. Refs and Images are assigned only when the App exists (typed-nil again);
	// without them every create answers 503 github_unavailable, audited.
	svcDeps := envs.ServiceDeps{
		Cfg: cfg, Store: envs.NewPGStore(pool, queries), Roles: roles,
		Catalog: cat, Enqueue: q,
	}
	if gh != nil {
		svcDeps.Refs, svcDeps.Images = gh, gh
	}
	envService := envs.NewService(svcDeps)

	// Preview access (ENV-09). Missing keys are not fatal: ForwardAuth then answers 503 and the
	// ticket endpoint answers 403, so previews fail CLOSED while the rest of the API works.
	tokens := &previewauth.Tokens{TicketTTL: cfg.PreviewTicketTTL, CookieTTL: cfg.PreviewCookieTTL}
	if keys, kerr := previewauth.LoadKeys(cfg); kerr != nil {
		slog.WarnContext(ctx, "preview.keys_unavailable", "error", kerr)
	} else {
		tokens.Keys = keys
	}

	prober, closeProbe, err := newProber(ctx, cfg)
	if err != nil {
		return err
	}
	defer closeProbe()
	go prober.Run(ctx)

	handler, _ := httpapi.New(httpapi.Deps{
		Config: cfg, Redis: rdb, Queue: q, Status: prober,
		Sessions: sessions, Queries: queries, Auth: authHandlers,
		Envs: envService, Catalog: cat,
		Preview: &previewauth.Handlers{
			Cfg: cfg, Tokens: tokens, Envs: httpapi.NewEnvLookup(queries), Sessions: sessions,
		},
		PreviewAuth: previewauth.NewForwardAuth(cfg, tokens, previewauth.RedisJTI{RDB: rdb}),
	})
	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: handler, ReadHeaderTimeout: 10 * time.Second}

	errCh := make(chan error, 1)
	go func() {
		slog.InfoContext(ctx, "api.listening", "addr", cfg.HTTPAddr, "version", buildinfo.Version)
		errCh <- srv.ListenAndServe()
	}()

	select {
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	case <-ctx.Done():
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		slog.InfoContext(sctx, "api.shutting_down")
		return srv.Shutdown(sctx)
	}
	return nil
}

// newProber builds the status prober. It owns uninstrumented Postgres and Redis
// connections so the probes do not create a trace every cycle.
func newProber(ctx context.Context, cfg config.Config) (*status.Prober, func(), error) {
	probePool, err := store.OpenUntraced(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("open probe pool: %w", err)
	}
	probeRedis := redis.NewClient(&redis.Options{Addr: cfg.RedisAddr})

	checks := status.Checks{
		Postgres: func(c context.Context) error { return probePool.Ping(c) },
		Redis:    func(c context.Context) error { return probeRedis.Ping(c).Err() },
		HeartbeatAt: func(c context.Context) (time.Time, error) {
			v, err := probeRedis.Get(c, queue.HeartbeatKey).Result()
			if err != nil {
				return time.Time{}, err
			}
			secs, err := strconv.ParseInt(v, 10, 64)
			if err != nil {
				return time.Time{}, err
			}
			return time.Unix(secs, 0), nil
		},
		HTTP: map[string]string{
			"dashboard":      cfg.WebHealthURL,
			"otel-collector": cfg.CollectorHealthURL,
			"tempo":          cfg.TempoReadyURL,
			"loki":           cfg.LokiReadyURL,
			"prometheus":     cfg.PrometheusReadyURL,
			"grafana":        cfg.GrafanaHealthURL,
		},
		LastSignal: status.NewLastSignal(cfg.PrometheusURL, cfg.ProbeTimeout),
	}
	p := status.NewProber(checks, cfg.ProbeInterval, cfg.ProbeTimeout, otel.Meter("ephematool/status"))
	return p, func() { probePool.Close(); _ = probeRedis.Close() }, nil
}
```
### 9.1 `deploy/compose.e2e.yaml` (replace)

Settings that exist **only** for the fake-GitHub stack (the file's header says so):

`deploy/compose.e2e.yaml`

```yaml
# Overlay for dev/e2e login with the fake GitHub. NOT for real deployments (T-01-26).
services:
  fakegithub:
    build:
      context: ..
      dockerfile: Dockerfile
      target: go-runtime
      args:
        CMD: fakegithub
    # The fakegithub binary has no subcommands; ignore the image default CMD.
    command: []
    environment:
      FAKEGITHUB_ADDR: ":9999"
    ports:
      - "127.0.0.1:9999:9999"

  api:
    environment:
      - GITHUB_CLIENT_ID=e2e-client
      - GITHUB_CLIENT_SECRET=e2e-secret
      # Browser-facing authorize URL; server-facing token and API URLs.
      - GITHUB_AUTHORIZE_URL=http://localhost:9999/login/oauth/authorize
      - GITHUB_TOKEN_URL=http://fakegithub:9999/login/oauth/access_token
      - GITHUB_API_URL=http://fakegithub:9999/
      - ALLOWED_ORGS=ephematool-e2e
      - ALLOWED_TEAMS=partner-org/sre
      - ADMIN_LOGINS=Octo-Admin
      # The ping throttle is global, so parallel specs would steal each other's window and a page
      # load would get no worker span. Disabled for e2e only (the default stays 10s).
      - PING_THROTTLE=0
      # ---- Phase 2 (DEV/E2E ONLY): the platform acts as a GitHub App against the fake ----
      # The fake checks the App JWT's shape, not its signature, so the dev key from
      # tools/stack.sh (ensure_secrets) is enough; no real credential is ever involved.
      - ALLOWED_REPOS=ephematool-e2e/demo-app
      - CREATOR_TEAMS=ephematool-e2e/creators
      - GITHUB_APP_ID=12345
      - GITHUB_APP_INSTALLATION_ID=777
      - GITHUB_APP_PRIVATE_KEY_FILE=/run/ephematool-secrets/github-app-dev.pem
      - GHCR_BASE_URL=http://fakegithub:9999
      - ENVIRONMENTS_REPO=ephematool-e2e/ephematool-environments
    depends_on:
      fakegithub:
        condition: service_started
```
### 9.2 Root `BUILD.bazel` (edit)

```python
test_suite(
    name = "test_integration",
    tests = [
        "//internal/auth:auth_test",
        "//internal/store:store_test",
        "//internal/audit:audit_test",
        "//internal/envs:envs_test",        # new
        "//internal/httpapi:httpapi_test",  # new (environments_integration_test.go)
    ],
)
```

---

## 10. Integration tests against real Postgres, Redis and the fake GitHub

The service runs for real here: the app-role pool, the real GitHub App client talking to the fake, the real catalog. Every refusal case asserts one audit row, **no environment row, nothing enqueued, and `Dispatches()` empty on the fake**: creating never reaches the workflow engine.

`internal/envs/create_integration_test.go`

```go
//go:build integration

package envs_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/catalog"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/envs"
	"github.com/ephematool/ephematool/internal/fakegithub"
	"github.com/ephematool/ephematool/internal/ghapp"
	"github.com/ephematool/ephematool/internal/testenv"
	"github.com/ephematool/ephematool/templates"
)

const repo = "ephematool-e2e/demo-app"

type recEnq struct {
	mu  sync.Mutex
	ids []string
}

func (r *recEnq) EnqueueEnvDispatch(_ context.Context, id string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ids = append(r.ids, id)
	return nil
}

// fixture wires the REAL service to real Postgres (app role) and the fake GitHub.
type fixture struct {
	t     *testing.T
	env   *testenv.Env
	fake  *fakegithub.Server
	svc   *envs.Service
	enq   *recEnq
	cfg   config.Config // the worker tests (phase 2c) build their own Worker from the same settings
	gh    *ghapp.Client
	start time.Time // DB clock at test start: audit rows are never truncated, so count only newer ones
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	env := testenv.Start(t)
	fake := fakegithub.New()
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	key, err := rsa.GenerateKey(rand.Reader, 2048) // the fake checks the JWT's shape, not its signature
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		GitHubAPIURL: srv.URL + "/", GHCRBaseURL: srv.URL, ImageTagFormat: "{sha}",
		GitHubAppID: 12345, GitHubAppInstallationID: 777, GitHubAppPrivateKeyFile: "unused",
		AllowedRepos: []string{repo}, CreatorTeams: []string{fakegithub.CreatorsTeam},
		AdminLogins: []string{"octo-admin"}, EnvDefaultTTL: 24 * time.Hour,
	}
	gh := ghapp.NewWithKey(cfg, key)
	cat, err := catalog.Load(templates.FS)
	if err != nil {
		t.Fatal(err)
	}
	enq := &recEnq{}
	svc := envs.NewService(envs.ServiceDeps{
		Cfg: cfg, Store: envs.NewPGStore(env.App, env.Queries), Roles: auth.NewRoleResolver(cfg, gh),
		Refs: gh, Images: gh, Catalog: cat, Enqueue: enq,
	})
	var start time.Time
	if err := env.Owner.QueryRow(context.Background(), `select now()`).Scan(&start); err != nil {
		t.Fatal(err)
	}
	return &fixture{t: t, env: env, fake: fake, svc: svc, enq: enq, cfg: cfg, gh: gh, start: start}
}

func (f *fixture) create(login string, id int64, req envs.CreateRequest) (string, error) {
	return f.svc.Create(context.Background(), auth.Principal{GithubID: id, Login: login}, req)
}

func goodReq() envs.CreateRequest {
	return envs.CreateRequest{Repo: repo, RefType: "branch", Ref: "main", Template: "web-app", Size: "S"}
}

func (f *fixture) auditCount(actor int64, outcome, reason string) int {
	f.t.Helper()
	var n int
	err := f.env.Owner.QueryRow(context.Background(),
		`select count(*) from audit_events where actor_github_id=$1 and outcome=$2 and reason=$3 and occurred_at >= $4`,
		actor, outcome, reason, f.start).Scan(&n)
	if err != nil {
		f.t.Fatal(err)
	}
	return n
}

func (f *fixture) envRows(owner int64) int {
	f.t.Helper()
	var n int
	if err := f.env.Owner.QueryRow(context.Background(),
		`select count(*) from environments where owner_github_id=$1 and created_at >= $2`, owner, f.start).Scan(&n); err != nil {
		f.t.Fatal(err)
	}
	return n
}

// noDispatch is the invariant of the whole slice: creating NEVER talks to the workflow engine.
func (f *fixture) noDispatch() {
	f.t.Helper()
	if d := f.fake.Dispatches(); len(d) != 0 {
		f.t.Fatalf("the API dispatched %d workflows; only the worker may", len(d))
	}
}

func rejection(t *testing.T, err error) *envs.Rejection {
	t.Helper()
	var rej *envs.Rejection
	if !errors.As(err, &rej) {
		t.Fatalf("want *Rejection, got %v", err)
	}
	return rej
}

func TestViewerDenied(t *testing.T) {
	f := newFixture(t)
	_, err := f.create("octo-viewer", 1002, goodReq())
	if rej := rejection(t, err); rej.Status != 403 || rej.Reason != "not_creator" {
		t.Fatalf("rej=%+v", rej)
	}
	if f.auditCount(1002, "denied", "not_creator") != 1 || f.envRows(1002) != 0 {
		t.Fatal("want one denied audit row and no environment row")
	}
	f.noDispatch()
}

func TestTeamCreatorCreates(t *testing.T) {
	f := newFixture(t)
	id, err := f.create("octo-creator", 1007, goodReq())
	if err != nil {
		t.Fatal(err)
	}
	var owner, image, status, traceID string
	var expires time.Time
	if err := f.env.Owner.QueryRow(context.Background(),
		`select owner_login, image, status, trace_id, expires_at from environments where id=$1`, id).
		Scan(&owner, &image, &status, &traceID, &expires); err != nil {
		t.Fatal(err)
	}
	if owner != "octo-creator" || status != "requested" || !strings.HasPrefix(image, "ghcr.io/"+repo+"@sha256:") {
		t.Fatalf("owner=%s status=%s image=%s", owner, status, image)
	}
	if d := time.Until(expires); d < 23*time.Hour || d > 25*time.Hour {
		t.Fatalf("expires_at in %v, want about 24h", d)
	}
	var auditTrace string
	if err := f.env.Owner.QueryRow(context.Background(),
		`select trace_id from audit_events where environment_id=$1 and outcome='success'`, id).Scan(&auditTrace); err != nil {
		t.Fatal(err)
	}
	if auditTrace != traceID {
		t.Fatalf("audit trace %q != row trace %q", auditTrace, traceID)
	}
	if len(f.enq.ids) != 1 || f.enq.ids[0] != id {
		t.Fatalf("enqueued=%v", f.enq.ids)
	}
	f.noDispatch()
}

func TestAdminCreates(t *testing.T) {
	f := newFixture(t)
	if _, err := f.create("octo-admin", 1001, goodReq()); err != nil {
		t.Fatal(err)
	}
}

// The demotion case from WR-01: no re-login, the very next request is refused.
func TestDemotedCreatorIsRefusedWithoutRelogin(t *testing.T) {
	f := newFixture(t)
	if _, err := f.create("octo-demoted", 1008, goodReq()); err != nil {
		t.Fatalf("first create: %v", err)
	}
	f.fake.SetTeamMembership("ephematool-e2e", "creators", "octo-demoted", "none")
	_, err := f.create("octo-demoted", 1008, goodReq())
	if rej := rejection(t, err); rej.Status != 403 {
		t.Fatalf("rej=%+v", rej)
	}
	if f.auditCount(1008, "denied", "not_creator") != 1 {
		t.Fatal("want one denied audit row for the demoted creator")
	}
}

func TestRefusalsAreAuditedAndNeverDispatch(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*envs.CreateRequest)
		status  int
		outcome string
		reason  string
	}{
		{"repo not allowed", func(r *envs.CreateRequest) { r.Repo = "x/y" }, 422, "denied", "repo_not_allowed"},
		{"missing branch", func(r *envs.CreateRequest) { r.Ref = "missing-x" }, 422, "rejected", "ref_not_found"},
		{"no image", func(r *envs.CreateRequest) { r.Ref = "noimage-x" }, 422, "rejected", "image_not_found"},
		{"fork PR", func(r *envs.CreateRequest) { r.RefType, r.Ref = "pr", "2" }, 422, "rejected", "pr_not_eligible"},
		{"deleted fork PR", func(r *envs.CreateRequest) { r.RefType, r.Ref = "pr", "3" }, 422, "rejected", "pr_not_eligible"},
		{"closed PR", func(r *envs.CreateRequest) { r.RefType, r.Ref = "pr", "4" }, 422, "rejected", "pr_not_eligible"},
		{"schema violation", func(r *envs.CreateRequest) { r.Values = map[string]any{"replicas": 9} }, 422, "rejected", "schema_violation"},
		{"bad ref", func(r *envs.CreateRequest) { r.Ref = "../x" }, 422, "rejected", "ref_invalid"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			req := goodReq()
			tc.mutate(&req)
			_, err := f.create("octo-creator", 1007, req)
			rej := rejection(t, err)
			if rej.Status != tc.status || rej.Outcome != tc.outcome || rej.Reason != tc.reason || !rej.Audited {
				t.Fatalf("rej=%+v", rej)
			}
			if f.auditCount(1007, tc.outcome, tc.reason) != 1 || f.envRows(1007) != 0 {
				t.Fatal("want exactly one audit row and no environment row")
			}
			if len(f.enq.ids) != 0 {
				t.Fatalf("enqueued %v on a refused request", f.enq.ids)
			}
			f.noDispatch()
		})
	}
}

func TestAppRoleCannotAlterAuditRows(t *testing.T) {
	f := newFixture(t)
	if _, err := f.create("octo-viewer", 1002, goodReq()); err == nil {
		t.Fatal("setup: viewer should be denied (that writes an audit row)")
	}
	_, err := f.env.App.Exec(context.Background(), `update audit_events set reason = 'tampered' where actor_github_id = 1002`)
	var pg *pgconn.PgError
	if !errors.As(err, &pg) || pg.Code != "42501" { // insufficient_privilege
		t.Fatalf("update as the app role: %v, want SQLSTATE 42501", err)
	}
}
```
`internal/httpapi/environments_integration_test.go`

```go
//go:build integration

package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/danielgtaylor/huma/v2/adapters/humago"

	"github.com/ephematool/ephematool/internal/auth"
	"github.com/ephematool/ephematool/internal/catalog"
	"github.com/ephematool/ephematool/internal/config"
	"github.com/ephematool/ephematool/internal/envs"
	"github.com/ephematool/ephematool/internal/testenv"
	"github.com/ephematool/ephematool/templates"
)

// readHarness serves the read operations over a real database, signed in as octo-creator (1007).
func readHarness(t *testing.T, env *testenv.Env) (http.Handler, *http.Cookie) {
	t.Helper()
	cat, err := catalog.Load(templates.FS)
	if err != nil {
		t.Fatal(err)
	}
	cfg := config.Config{
		SessionIdleTimeout: time.Hour, SessionLifetime: 2 * time.Hour, EnvDefaultTTL: 24 * time.Hour,
		PreviewScheme: "http", PreviewDomain: "preview.localhost", PreviewPort: 8088,
	}
	sm := auth.NewSessionManager(env.Redis, cfg)
	mux := http.NewServeMux()
	api := humago.New(mux, huma.DefaultConfig("test", "0"))
	api.UseMiddleware(auth.RequireSession(api, sm))
	mux.HandleFunc("POST /test/login", func(w http.ResponseWriter, r *http.Request) {
		_ = sm.RenewToken(r.Context())
		sm.Put(r.Context(), auth.KeyUserID, int64(1007))
		sm.Put(r.Context(), auth.KeyLogin, "octo-creator")
	})
	registerEnvironments(api, Deps{Config: cfg, Sessions: sm, Queries: env.Queries, Catalog: cat})
	h := sm.LoadAndSave(mux)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("POST", "/test/login", nil))
	for _, c := range rec.Result().Cookies() {
		if c.Name == auth.CookieName {
			return h, c
		}
	}
	t.Fatal("no session cookie")
	return nil, nil
}

func get(h http.Handler, c *http.Cookie, path string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("GET", path, nil)
	req.AddCookie(c)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// seed inserts an environment as the owner, returning its id.
func seed(t *testing.T, env *testenv.Env, owner int64, status string) string {
	t.Helper()
	id := envs.NewID()
	_, err := env.Owner.Exec(context.Background(), `
		insert into environments (id, owner_github_id, owner_login, repo, ref_type, ref, sha, image, template,
		  chart_version, size, correlation_id, traceparent, trace_id, expires_at, status, running_at)
		values ($1,$2,'octo-creator','ephematool-e2e/demo-app','branch','main',repeat('a',40),'ghcr.io/x@sha256:'||repeat('b',64),
		  'web-app','0.1.0','M','cr-1','','0af7651916cd43dd8448eb211c80319c', now()+interval '24 hours', $3,
		  case when $3='running' then now() end)`, id, owner, status)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestGetEnvironment(t *testing.T) {
	env := testenv.Start(t)
	h, c := readHarness(t, env)

	prov, run := seed(t, env, 1007, "provisioning"), seed(t, env, 1007, "running")

	var b EnvironmentBody
	rec := get(h, c, "/api/environments/"+run)
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &b) != nil {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if b.PreviewURL != "http://"+run+".preview.localhost:8088" || b.Size.CPU != "1 vCPU" || b.RunningAt == nil {
		t.Fatalf("running=%+v", b)
	}
	rec = get(h, c, "/api/environments/"+prov)
	_ = json.Unmarshal(rec.Body.Bytes(), &b)
	if b.PreviewURL != "" {
		t.Fatalf("provisioning env exposed a preview URL: %q", b.PreviewURL)
	}
	if got := get(h, c, "/api/environments/pv-zzzzzzzzzz").Code; got != 404 {
		t.Fatalf("unknown id -> %d, want 404", got)
	}
}

func TestListIsScopedToCallerNewestFirst(t *testing.T) {
	env := testenv.Start(t)
	h, c := readHarness(t, env)
	mine1 := seed(t, env, 1007, "requested")
	time.Sleep(10 * time.Millisecond)
	mine2 := seed(t, env, 1007, "requested")
	other := seed(t, env, 9999, "requested") // somebody else's

	rec := get(h, c, "/api/environments?limit=2")
	var out struct {
		Environments []EnvironmentSummary `json:"environments"`
	}
	if rec.Code != 200 || json.Unmarshal(rec.Body.Bytes(), &out) != nil || len(out.Environments) != 2 {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body)
	}
	if out.Environments[0].ID != mine2 || out.Environments[1].ID != mine1 {
		t.Fatalf("order=%v want newest first [%s %s]", out.Environments, mine2, mine1)
	}
	if strings.Contains(rec.Body.String(), other) {
		t.Fatal("list leaked another user's environment")
	}
	if got := get(h, c, "/api/environments?limit=21").Code; got != 422 {
		t.Fatalf("limit=21 -> %d, want 422 (bounded)", got)
	}
}
```
```bash
bazel run //:gazelle
bazel test --config=integration //internal/envs:envs_test //internal/httpapi:httpapi_test
bazel test --config=integration //:test_integration
```

---

## 11. Regenerate the API contract and run everything

```bash
bazel run //:gen                       # api/openapi.json and web/src/api/schema.d.ts
grep -c '"create-environment"\|"get-environment"\|"list-environments"\|"get-environment-options"\|"post-preview-ticket"' api/openapi.json   # 5
grep -n 'creatorTeams' web/src/api/schema.d.ts | head -3                  # the CreateError body type exists
bazel test //:check_gen //web:typecheck
bazel test //...
bazel test --config=integration //:test_integration
bazel run //:up_fake                   # still healthy; the new collector pipelines start
bash scripts/verify.d/env-telemetry.sh
bazel run //:down
```

Try the API by hand against the fake stack (sign in as `octo-creator` in the browser first, or use the helper that `scripts/lib/verify-lib.sh` provides):

```bash
. scripts/lib/verify-lib.sh && ensure_session octo-creator
curl -s -b "$VERIFY_TMP/cookies" -H 'Content-Type: application/json' -H 'Origin: http://localhost:8080' \
  -d '{"repo":"ephematool-e2e/demo-app","refType":"branch","ref":"main","template":"web-app","size":"S"}' \
  http://127.0.0.1:8080/api/environments        # {"id":"pv-..."}
curl -s -b "$VERIFY_TMP/cookies" -H 'Content-Type: application/json' -H 'Origin: http://localhost:8080' \
  -d '{"repo":"ephematool-e2e/demo-app","refType":"branch","ref":"main","template":"web-app","size":"S","values":{"replicas":9}}' \
  http://127.0.0.1:8080/api/environments        # 422 with values.replicas
```

(The row stays `requested` until the worker arrives in 2c; the sweeper picks it up then.)

---

## 12. Verification status of this document's code

| Part | Status |
|---|---|
| `templates/` chart schema, defaults and the four behaviours of `values.schema.json` (accepts defaults plus an image; rejects `replicas: 9`, an extra key, a bad enum value, a bad image) | Checked with Python `jsonschema` (draft-07). `sizes.yaml` parsed. **Helm itself was not run** (not installed), and the chart templates were rendered only with a small stand-in for Helm's `text/template` renderer (no sprig functions beyond the few used), which catches template mistakes but is not Helm. |
| `internal/catalog/form.go` and `form_test.go` | Compiled and run with `go test` (stdlib only): pass. |
| `internal/catalog/{catalog,validate,sizes}.go` and `catalog_test.go` | Written to the jsonschema/v6 and yaml.v3 APIs and syntax-checked with `gofmt`; **not compiled** (modules could not be downloaded). |
| `internal/previewauth` keys, tokens, safereturn, forwardauth and their tests (41 cases) | Compiled and run with `go test -race` against a **small stand-in for `golang-jwt/jwt/v5`** that implements only the subset used. Real-library behaviour (option names, `kid` handling) is therefore **not** proven. |
| `internal/previewauth/{ticket,redisjti}.go`, `ticket_test.go` | Syntax-checked only (huma, scs, go-redis, miniredis). |
| `internal/envs` service and unit tests | Compiled and run with `go test -race` with **stand-ins** for the catalog, auth, audit and OpenTelemetry (a no-op `telemetry.go`): the check order, audit counts and zero-enqueue invariants pass. `pgstore.go`, the real `telemetry.go` and the integration tests were syntax-checked only. |
| `internal/queue` additions and test | Syntax-checked only (asynq, miniredis). |
| `internal/httpapi/environments.go`, `api.go`, tests | Syntax-checked only (huma, pgx, sqlc types). The `Responses` declaration that makes `CreateError` appear in the OpenAPI document is the least certain part: after `bazel run //:gen`, confirm `schema.d.ts` contains it. |
| `cmd/api/main.go`, `cmd/sampleapp` | `gofmt` only. The sample app's request handler logic was run in a stdlib-only copy. |
| `deploy/otel/collector.yaml` | Parsed as YAML. **Not validated by the collector** (no Docker daemon here); the shape follows the pipeline verified in the research phase. |
| `deploy/compose.yaml` collector edit and the e2e overlay | Validated with `docker compose config`. |

---

## 13. Acceptance checklist

- [ ] `helm lint` passes and `helm template … --set replicas=9` fails (section 2.4); no hand-written `ephematool.io/` or `ephematool.env.` literal under `templates/`.
- [ ] `bazel test //internal/catalog/...` passes every case, including the two-level object and required-unsupported cases.
- [ ] `bazel test //internal/previewauth/...` passes; `grep -n 'WithValidMethods' internal/previewauth/tokens.go` and `grep -n 'WithExpirationRequired' internal/previewauth/tokens.go` match.
- [ ] `grep -n 'Domain' internal/previewauth/forwardauth.go` shows no cookie `Domain` assignment.
- [ ] `docker run … otel/opentelemetry-collector-contrib:0.162.0 validate --config=/cfg/collector.yaml` exits 0 (command in `env-telemetry.sh`); `bash scripts/verify.d/env-telemetry.sh` prints three PASS lines on the running stack.
- [ ] `grep -n 'limit_mib: 300' deploy/otel/collector.yaml` and `grep -n 'max_request_body_size: 1048576' deploy/otel/collector.yaml` match; `grep -n 'ephematool\.env\.' deploy/otel/collector.yaml` prints nothing.
- [ ] `bazel test //cmd/sampleapp/...` passes; `grep -n 'telemetry.Setup' cmd/sampleapp/main.go` prints nothing.
- [ ] `bazel test //internal/envs/... //internal/queue/... //internal/httpapi/...` passes.
- [ ] `grep -n 'ephematool\.env\.\|ephematool\.io/' internal/envs/*.go | grep -v _test` prints nothing (convention constants only).
- [ ] `bazel test --config=integration //:test_integration` passes, including the demoted-creator case and the "fake recorded zero dispatches" checks.
- [ ] `bazel run //:gen` leaves `git diff --stat` showing only `api/openapi.json` and `web/src/api/schema.d.ts`; `bazel test //:check_gen` passes.
- [ ] A create as `octo-creator` returns `202 {id}` and the row is `requested`; as `octo-viewer` returns `403` with `creatorTeams`; with `replicas: 9` returns `422` naming `values.replicas`.

---

## 14. Common failures

| Symptom | Likely cause | Fix |
|---|---|---|
| `_helpers.tpl` missing at runtime (`catalog.Load` error "no such file") | embed directive lacks `all:` or `embedsrcs` omits the file | keep `//go:embed sizes.yaml all:web-app` and list every chart file in `templates/BUILD.bazel` |
| panic in `LocalizedString` | nil message printer | always `message.NewPrinter(language.English)` (already in `validate.go`) |
| `jsonschema: invalid jsonType: int` | values passed to `Validate` without normalising | keep the JSON round trip in `ValidateValues` |
| every create answers `503 github_unavailable` | GitHub App not configured, or the fake is not reachable | set the three `GITHUB_APP_*` values; for e2e check `compose.e2e.yaml` and `GHCR_BASE_URL` |
| `/api/preview/ticket` answers `403` for a real environment | signing key not loaded (`preview.keys_unavailable` in the log) | `ensure_secrets` creates `deploy/.secrets/preview-token.key`; check the compose mount and `PREVIEW_TOKEN_KEY_FILE` |
| ForwardAuth answers `503` for everything except health | same as above (fails closed) | same |
| preview app still receives `ephematool_preview` | Traefik `authResponseHeaders` lacks `Cookie` | set in 2c (`authResponseHeaders: [Cookie]`) |
| collector exits with `unknown value ${env:EPHEMATOOL_ATTR_ENV_ID}` | compose `env_file` missing | add the `env_file` block from section 5.2 |
| `create-environment` 422 body in the UI has no `field` | the request failed Huma's own validation (malformed JSON, field too long) | handle both shapes in the form (2d) |
| `TestEveryOperationIsProtectedOrPublic` panics | a register function dereferences a nil dependency | registration must be nil-safe; only handler bodies may use `deps` |
| integration test counts are off | audit rows are never truncated | tests count rows newer than the DB clock at test start (`f.start`); use unique actors |
| `sampleapp` spans dropped by the collector | it set `service.namespace=ephematool` or has no env id | use `resource.Default()` and `OTEL_RESOURCE_ATTRIBUTES` only |
