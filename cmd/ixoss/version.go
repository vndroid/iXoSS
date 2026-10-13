package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"strings"
)

// Version is the project's SemVer, independent of the imgproxy port baseline.
const Version = "0.1.0"

// Build metadata can be supplied with go build -ldflags "-X main.Name=value".
var (
	Channel   = "DEVELOPMENT"
	BuildDate = "unknown"
	Revision  = "unknown"
)

func buildRevision() string {
	if Revision != "unknown" {
		return Revision
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return Revision
	}
	revision, modified := Revision, false
	for _, setting := range info.Settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.modified":
			modified = setting.Value == "true"
		}
	}
	if modified && revision != "unknown" {
		revision += "-dirty"
	}
	return revision
}

func versionInfo(colored bool) string {
	first := fmt.Sprintf("iXoSS version %s (%s)", Version, Channel)
	lines := [][2]string{
		{"Built:", BuildDate},
		{"Revision:", buildRevision()},
		{"Platform:", runtime.GOOS + "/" + runtime.GOARCH},
		{"Runtime:", runtime.Version()},
		{"Copyright:", "2026 Kane"},
	}
	if colored {
		first = bold(first)
	}
	var out strings.Builder
	out.WriteString(first + "\n")
	for _, line := range lines {
		if colored {
			out.WriteString("  " + cyan(line[0]) + bold(" "+line[1]) + "\n")
		} else {
			out.WriteString("  " + line[0] + " " + line[1] + "\n")
		}
	}
	return out.String()
}

// Cyan (#22d3ee) highlights labels; values use bold text.
func cyan(value string) string { return "\x1b[38;2;34;211;238m" + value + "\x1b[0m" }
func bold(value string) string { return "\x1b[1m" + value + "\x1b[0m" }

func colorEnabled(output *os.File) bool {
	return os.Getenv("NO_COLOR") == "" && os.Getenv("TERM") != "dumb" && isTerminal(output)
}
