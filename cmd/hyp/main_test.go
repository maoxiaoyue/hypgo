package main

import (
	"testing"
)

func TestPluginRegistry_RegisterAndGet(t *testing.T) {
	registry := GetRegistry()

	// Register a dummy plugin
	dummy := PluginMetadata{
		Name:        "test-plugin",
		Version:     "1.0.0",
		Category:    "Test",
		Description: "A test plugin",
	}
	registry.Register(dummy)

	// Retrieve the plugin
	retrieved, ok := registry.Get("test-plugin")
	if !ok {
		t.Fatalf("Expected plugin to be registered, but not found")
	}

	if retrieved.Name != dummy.Name || retrieved.Version != dummy.Version {
		t.Errorf("Retrieved plugin metadata does not match: got %+v, want %+v", retrieved, dummy)
	}
}

func TestPluginRegistry_ListByCategory(t *testing.T) {
	registry := GetRegistry()

	plugins := registry.ListByCategory("Test")
	found := false
	for _, p := range plugins {
		if p.Name == "test-plugin" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("Expected to find test-plugin in Category 'Test'")
	}
}

func TestPluginRegistry_Categories(t *testing.T) {
	registry := GetRegistry()
	categories := registry.Categories()
	if len(categories) == 0 {
		t.Errorf("Expected categories to not be empty")
	}
}

func TestRegisterCommands(t *testing.T) {
	// The init function already called registerCommands()
	// Let's verify that root commands have been set
	if !rootCmd.HasSubCommands() {
		t.Errorf("Expected rootCmd to have subcommands")
	}
}
