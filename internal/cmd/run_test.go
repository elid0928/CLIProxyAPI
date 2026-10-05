package cmd

import (
	"testing"

	"github.com/router-for-me/CLIProxyAPI/v8/internal/config"
)

func TestStartServiceBackgroundInitializesAndClosesUsageBackendsOnBuildFailure(t *testing.T) {
	originalInitializer := initializeUsageBackendsForService
	t.Cleanup(func() {
		initializeUsageBackendsForService = originalInitializer
	})

	initializeCalls := 0
	closeCalls := 0
	initializeUsageBackendsForService = func(cfg *config.Config) func() {
		initializeCalls++
		if cfg == nil {
			t.Fatal("usage backend initializer received nil config")
		}
		return func() {
			closeCalls++
		}
	}

	cancel, done := StartServiceBackgroundWithPluginHost(&config.Config{}, "", "", nil)
	cancel()
	<-done

	if initializeCalls != 1 {
		t.Fatalf("usage backend initialization calls = %d, want 1", initializeCalls)
	}
	if closeCalls != 1 {
		t.Fatalf("usage backend close calls = %d, want 1", closeCalls)
	}
}
