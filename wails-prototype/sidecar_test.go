package main

import "testing"

func TestResolveRuntimeModeDefaultsToGoSidecar(t *testing.T) {
	t.Setenv(sidecarRuntimeEnvKey, "")

	if got, want := resolveRuntimeMode(), runtimeModeGoSidecar; got != want {
		t.Fatalf("resolveRuntimeMode() = %q, want %q", got, want)
	}
}

func TestResolveRuntimeModeSupportsMock(t *testing.T) {
	t.Setenv(sidecarRuntimeEnvKey, "mock")

	if got, want := resolveRuntimeMode(), runtimeModeMock; got != want {
		t.Fatalf("resolveRuntimeMode() = %q, want %q", got, want)
	}
}
