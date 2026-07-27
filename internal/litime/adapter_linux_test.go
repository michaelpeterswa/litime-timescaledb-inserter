//go:build linux

package litime

import (
	"testing"
	"time"
)

// Naming an adapter is how a USB dongle gets used instead of the onboard radio,
// so it has to be accepted and has to produce a distinct adapter.
func TestResolveAdapterAcceptsName(t *testing.T) {
	t.Parallel()

	named, err := resolveAdapter("hci1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if named == nil {
		t.Fatal("resolveAdapter returned nil for a named adapter")
	}

	def, err := resolveAdapter("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Falling back to the default would silently use the onboard radio, which
	// is the exact thing naming an adapter is meant to avoid.
	if named == def {
		t.Error("named adapter resolved to the default adapter")
	}
}

func TestNewManagerAcceptsAdapterName(t *testing.T) {
	t.Parallel()

	_, err := NewManager(ManagerOptions{
		Batteries:      []Battery{{ID: "garage", Match: MatchName, Value: "battery"}},
		ScrapeInterval: time.Second,
		StaleTimeout:   10 * time.Second,
		AdapterName:    "hci1",
	})
	if err != nil {
		t.Fatalf("naming an adapter should be accepted on linux, got: %v", err)
	}
}
