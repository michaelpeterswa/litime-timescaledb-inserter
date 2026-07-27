//go:build !linux

package litime

import (
	"testing"
	"time"
)

// Only Linux can select an adapter by name. Elsewhere the request must fail
// loudly rather than fall back to the default, which would use a different
// radio than the operator asked for.
func TestResolveAdapterRejectsNameOffLinux(t *testing.T) {
	t.Parallel()

	if _, err := resolveAdapter("hci1"); err == nil {
		t.Error("expected an error naming an adapter on a platform without adapter selection")
	}
}

func TestNewManagerRejectsAdapterNameOffLinux(t *testing.T) {
	t.Parallel()

	_, err := NewManager(ManagerOptions{
		Batteries:      []Battery{{ID: "garage", Match: MatchName, Value: "battery"}},
		ScrapeInterval: time.Second,
		StaleTimeout:   10 * time.Second,
		AdapterName:    "hci1",
	})
	if err == nil {
		t.Error("expected NewManager to reject a named adapter on this platform")
	}
}
