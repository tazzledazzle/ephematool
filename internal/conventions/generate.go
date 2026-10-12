// Package conventions exposes the D-09 Kubernetes label and OpenTelemetry
// attribute names, generated from conventions/telemetry.json.
package conventions

//go:generate go run ../../tools/genconventions -in ../../conventions/telemetry.json -go-out conventions.gen.go -ts-out ../../web/src/gen/conventions.gen.ts
