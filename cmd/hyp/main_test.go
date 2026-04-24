package main

import (
	"testing"
)

func TestRegisterCommands(t *testing.T) {
	// The init function already called registerCommands()
	// Let's verify that root commands have been set
	if !rootCmd.HasSubCommands() {
		t.Errorf("Expected rootCmd to have subcommands")
	}
}
