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
	t.Parallel()

	tmp := t.TempDir()
	if err := os.MkdirAll(filepath.Join(tmp, "pkg", "sub"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(tmp, "other"), 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	targets, err := expandTargets([]string{"./..."})
	if err != nil {
		t.Fatalf("expandTargets returned error: %v", err)
	}
	want := []string{".", "other", "pkg", filepath.Join("pkg", "sub")}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("unexpected targets %v, want %v", targets, want)
	}
}
func TestExpandTargetsParentEllipsis(t *testing.T) {
	t.Parallel()

	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer func() {
		_ = os.Chdir(cwd)
	}()

	tmp := t.TempDir()
	project := filepath.Join(tmp, "Projects", "frankenphp")
	if err := os.MkdirAll(project, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.Chdir(filepath.Join(tmp, "Projects")); err != nil {
		t.Fatalf("chdir: %v", err)
	}

	targets, err := expandTargets([]string{"../Projects/frankenphp/..."})
	if err != nil {
		t.Fatalf("expandTargets returned error: %v", err)
	}
	want := []string{"../Projects/frankenphp"}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("unexpected targets %v, want %v", targets, want)
	}
}
