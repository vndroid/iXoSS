package main

import (
	"flag"
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestCLIArgumentHelper(t *testing.T) {
	if os.Getenv("IXOSS_ARGUMENT_HELPER") != "1" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("ixoss", flag.ExitOnError)
	os.Args = append([]string{"ixoss"}, strings.Split(os.Getenv("IXOSS_TEST_ARGS"), "|")...)
	main()
	os.Exit(0)
}

func TestUnexpectedArgumentsFailBeforeStartup(t *testing.T) {
	for _, args := range []string{"foo|--version", "ixoss|--version", "--version|extra"} {
		cmd := exec.Command(os.Args[0], "-test.run=^TestCLIArgumentHelper$")
		cmd.Env = append(os.Environ(), "IXOSS_ARGUMENT_HELPER=1", "IXOSS_TEST_ARGS="+args, "IXOSS_ROOT=")
		out, err := cmd.CombinedOutput()
		exit, ok := err.(*exec.ExitError)
		if !ok || exit.ExitCode() != 2 || !strings.Contains(string(out), "unexpected argument") || strings.Contains(string(out), "listening") {
			t.Fatalf("%s: %v %s", args, err, out)
		}
	}
}
