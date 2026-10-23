package main

import "testing"

func TestResolveVersionPrefersLdflags(t *testing.T) {
	if got := resolveVersion("v1.2.3"); got != "v1.2.3" {
		t.Errorf("resolveVersion(v1.2.3) = %q", got)
	}
}

func TestResolveVersionFallsBack(t *testing.T) {
	// Test binaries carry no module version, so this exercises the fallback.
	if got := resolveVersion(""); got == "" {
		t.Error("resolveVersion returned empty string")
	}
}
