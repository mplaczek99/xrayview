package main

import "testing"

func TestNewSidecarTransportRetainsBurstIdleConnections(t *testing.T) {
	transport := newSidecarTransport()

	if got, want := transport.MaxIdleConns, sidecarMaxIdleConns; got != want {
		t.Fatalf("MaxIdleConns = %d, want %d", got, want)
	}
	if got, want := transport.MaxIdleConnsPerHost, sidecarMaxIdleConns; got != want {
		t.Fatalf("MaxIdleConnsPerHost = %d, want %d", got, want)
	}
}

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
