package httpapi

import (
	"sort"
	"testing"

	"github.com/danielgtaylor/huma/v2"

	"github.com/ephematool/ephematool/internal/auth"
)

// operations lists every operation of an OpenAPI document by operation id.
func operations(oa *huma.OpenAPI) map[string]*huma.Operation {
	out := map[string]*huma.Operation{}
	for _, item := range oa.Paths {
		for _, op := range []*huma.Operation{item.Get, item.Put, item.Post, item.Delete, item.Options, item.Head, item.Patch, item.Trace} {
			if op != nil {
				out[op.OperationID] = op
			}
		}
	}
	return out
}

// Every operation must either require the session scheme or be explicitly
// public. New routes therefore cannot silently ship unauthenticated (WR-02).
// Note: New is called with nil deps, so every register function must be nil-safe.
func TestEveryOperationIsProtectedOrPublic(t *testing.T) {
	_, api := New(Deps{})
	var public []string
	for id, op := range operations(api.OpenAPI()) {
		isPublic, _ := op.Extensions[auth.PublicExtension].(bool)
		hasSession := false
		for _, req := range op.Security {
			if _, ok := req["session"]; ok {
				hasSession = true
			}
		}
		if !hasSession && !isPublic {
			t.Errorf("operation %q has neither session Security nor x-public", id)
		}
		if isPublic {
			public = append(public, id)
		}
	}
	sort.Strings(public)
	want := []string{"get-auth-config", "post-auth-logout"}
	if len(public) != len(want) || public[0] != want[0] || public[1] != want[1] {
		t.Fatalf("public operations = %v, want exactly %v", public, want)
	}
}
