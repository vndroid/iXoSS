package main

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestVersionInfoPlainAndColored(t *testing.T) {
	plain := versionInfo(false)
	if !strings.HasPrefix(plain, "iXoSS version 0.1.0 (DEVELOPMENT)\n") || strings.Contains(plain, "\x1b") {
		t.Fatalf("unexpected plain version: %q", plain)
	}
	colored := versionInfo(true)
	if !strings.Contains(colored, "\x1b[38;2;34;211;238m") {
		t.Fatal("missing cyan highlight")
	}
	if stripped := regexp.MustCompile(`\x1b\[[0-9;]*m`).ReplaceAllString(colored, ""); stripped != plain {
		t.Fatalf("color changes version content: %q", stripped)
	}
}

func TestColorDisabledForNonTerminal(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	t.Setenv("TERM", "xterm-256color")
	for _, path := range []string{os.DevNull, t.TempDir() + "/output"} {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		if colorEnabled(f) {
			t.Errorf("non-terminal %s enables colors", path)
		}
		f.Close()
	}
}

func TestBuildRevisionOverride(t *testing.T) {
	original := Revision
	t.Cleanup(func() { Revision = original })
	Revision = "test-revision"
	if got := buildRevision(); got != Revision {
		t.Fatalf("injected revision ignored: %q", got)
	}
}
