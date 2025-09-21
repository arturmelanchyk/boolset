package main

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExpandTargetsDefault(t *testing.T) {
	t.Parallel()

	targets, err := expandTargets(nil)
	if err != nil {
		t.Fatalf("expandTargets returned error: %v", err)
	}
	want := []string{"."}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("unexpected targets %v, want %v", targets, want)
	}
}

func TestExpandTargetsEllipsis(t *testing.T) {
	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "pkg", "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "other"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	withWorkingDir(t, tmp)

	targets, err := expandTargets([]string{"./..."})
	if err != nil {
		t.Fatalf("expandTargets returned error: %v", err)
	}
	want := []string{".", "other", "pkg", filepath.Join("pkg", "sub")}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("unexpected targets %v, want %v", targets, want)
	}
}

func withWorkingDir(t *testing.T, dir string) {
	t.Helper()
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("chdir: %v", err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(cwd); err != nil {
			t.Fatalf("cleanup chdir: %v", err)
		}
	})
}
