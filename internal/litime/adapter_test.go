package litime

import "testing"

// An unset adapter must keep working exactly as before on every platform, since
// that is what existing deployments rely on.
func TestResolveAdapterDefaultsWhenUnset(t *testing.T) {
	t.Parallel()

	adapter, err := resolveAdapter("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if adapter == nil {
		t.Fatal("resolveAdapter returned a nil adapter for the default")
	}
}
