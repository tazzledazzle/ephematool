package auth

import (
	"context"
	"errors"
	"testing"
)

type fakeMC struct {
	orgs  map[string][2]any // org -> {state, status}
	teams map[string][2]any
	err   error
}

func (f fakeMC) OrgMembership(_ context.Context, org string) (string, int, error) {
	if f.err != nil {
		return "", 0, f.err
	}
	v, ok := f.orgs[org]
	if !ok {
		return "", 404, nil
	}
	return v[0].(string), v[1].(int), nil
}

func (f fakeMC) TeamMembership(_ context.Context, org, slug, _ string) (string, int, error) {
	if f.err != nil {
		return "", 0, f.err
	}
	v, ok := f.teams[org+"/"+slug]
	if !ok {
		return "", 404, nil
	}
	return v[0].(string), v[1].(int), nil
}

func TestAllowlist(t *testing.T) {
	al := Allowlist{Orgs: []string{"acme"}, Teams: []string{"partner/sre"}}
	cases := []struct {
		name      string
		al        Allowlist
		mc        fakeMC
		allowed   bool
		forbidden bool
		granted   []string
	}{
		{"org active", al, fakeMC{orgs: map[string][2]any{"acme": {"active", 200}}}, true, false, []string{"acme"}},
		{"org pending", al, fakeMC{orgs: map[string][2]any{"acme": {"pending", 200}}}, false, false, nil},
		{"org 404", al, fakeMC{}, false, false, nil},
		{"org 403", al, fakeMC{orgs: map[string][2]any{"acme": {"", 403}}}, false, true, nil},
		{"transport error", al, fakeMC{err: errors.New("boom")}, false, false, nil},
		{"team active", al, fakeMC{teams: map[string][2]any{"partner/sre": {"active", 200}}}, true, false, []string{"partner/sre"}},
		{"empty allowlist", Allowlist{}, fakeMC{orgs: map[string][2]any{"acme": {"active", 200}}}, false, false, nil},
		{"403 org but team ok", al, fakeMC{orgs: map[string][2]any{"acme": {"", 403}}, teams: map[string][2]any{"partner/sre": {"active", 200}}}, true, false, []string{"partner/sre"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := c.al.Evaluate(context.Background(), c.mc, "u")
			if d.Allowed != c.allowed || d.Forbidden != c.forbidden {
				t.Fatalf("got %+v", d)
			}
			if len(d.GrantedBy) != len(c.granted) || (len(c.granted) > 0 && d.GrantedBy[0] != c.granted[0]) {
				t.Fatalf("granted %v, want %v", d.GrantedBy, c.granted)
			}
			if !d.Allowed && d.Reason != "org_membership" {
				t.Fatalf("reason %q", d.Reason)
			}
		})
	}
}
