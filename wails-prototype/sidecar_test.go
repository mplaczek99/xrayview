package main

import (
	"path/filepath"
	"testing"
)

func TestFindRepoRoot(t *testing.T) {
	repoRoot, err := resolveRepoRoot()
	if err != nil {
		t.Fatalf("resolveRepoRoot() error = %v", err)
	}

	start := filepath.Join(repoRoot, "wails-prototype", "build", "bin")
	found, ok := findRepoRoot(start)
	if !ok {
		t.Fatalf("findRepoRoot(%q) did not locate the repository root", start)
	}

	if found != repoRoot {
		t.Fatalf("findRepoRoot(%q) = %q, want %q", start, found, repoRoot)
	}
}
