package main

import "testing"

func TestResolveRuntimeModeDefaultsToDesktop(t *testing.T) {
	t.Setenv(sidecarRuntimeEnvKey, "")

	if got, want := resolveRuntimeMode(), runtimeModeDesktop; got != want {
		t.Fatalf("resolveRuntimeMode() = %q, want %q", got, want)
	}
}

func TestResolveRuntimeModeSupportsDesktop(t *testing.T) {
	t.Setenv(sidecarRuntimeEnvKey, "desktop")

	if got, want := resolveRuntimeMode(), runtimeModeDesktop; got != want {
		t.Fatalf("resolveRuntimeMode() = %q, want %q", got, want)
	}
}

func TestResolveRuntimeModeSupportsMock(t *testing.T) {
	t.Setenv(sidecarRuntimeEnvKey, "mock")

	if got, want := resolveRuntimeMode(), runtimeModeMock; got != want {
		t.Fatalf("resolveRuntimeMode() = %q, want %q", got, want)
	}
}
