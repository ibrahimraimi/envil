package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRootCommandHelpAndFlags(t *testing.T) {
	rootCmd := buildRootCommand()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"--help"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute --help failed: %v", err)
	}

	helpText := buf.String()
	if !strings.Contains(helpText, "--no-tui") {
		t.Fatalf("expected help text to contain '--no-tui', got: %s", helpText)
	}
	if !strings.Contains(helpText, "tui") {
		t.Fatalf("expected help text to contain 'tui' command, got: %s", helpText)
	}
}

func TestTUICommandRequiresTTY(t *testing.T) {
	rootCmd := buildRootCommand()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{"tui"})

	err := rootCmd.Execute()
	if err == nil {
		t.Fatalf("expected error when running envil tui in non-interactive environment, got nil")
	}

	if !strings.Contains(err.Error(), "requires an interactive terminal") {
		t.Fatalf("expected error mentioning interactive terminal, got: %v", err)
	}
}

func TestBareEnvilInNonTTYPrintsHelp(t *testing.T) {
	rootCmd := buildRootCommand()
	buf := new(bytes.Buffer)
	rootCmd.SetOut(buf)
	rootCmd.SetErr(buf)
	rootCmd.SetArgs([]string{})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("execute bare command failed: %v", err)
	}

	output := buf.String()
	if !strings.Contains(output, "Usage:") {
		t.Fatalf("expected help output for bare invocation in non-TTY, got: %s", output)
	}
}
