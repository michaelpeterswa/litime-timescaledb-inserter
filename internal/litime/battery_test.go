package litime

import "testing"

func TestParseBatteriesFallsBackToSingleName(t *testing.T) {
	t.Parallel()

	batteries, err := ParseBatteries(nil, "LiTime Battery")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(batteries) != 1 {
		t.Fatalf("got %d batteries, want 1", len(batteries))
	}

	want := Battery{ID: "LiTime Battery", Match: MatchName, Value: "LiTime Battery"}
	if batteries[0] != want {
		t.Errorf("got %+v, want %+v", batteries[0], want)
	}
}

func TestParseBatteriesPrefersExplicitList(t *testing.T) {
	t.Parallel()

	batteries, err := ParseBatteries(map[string]string{"garage": "battery-one"}, "ignored-fallback")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(batteries) != 1 {
		t.Fatalf("got %d batteries, want 1", len(batteries))
	}

	if batteries[0].ID != "garage" || batteries[0].Value != "battery-one" {
		t.Errorf("got %+v, want the configured list to win over the fallback", batteries[0])
	}
}

func TestParseBatteriesSortsByID(t *testing.T) {
	t.Parallel()

	// Map iteration is randomised, so an unsorted implementation would only fail
	// this intermittently. Enough entries to make that unlikely to slip through.
	entries := map[string]string{
		"shed":    "d",
		"garage":  "b",
		"attic":   "a",
		"cellar":  "c",
		"kitchen": "e",
	}

	batteries, err := ParseBatteries(entries, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := []string{"attic", "cellar", "garage", "kitchen", "shed"}
	if len(batteries) != len(want) {
		t.Fatalf("got %d batteries, want %d", len(batteries), len(want))
	}

	for i, id := range want {
		if batteries[i].ID != id {
			t.Errorf("battery %d is %q, want %q", i, batteries[i].ID, id)
		}
	}
}

func TestParseBatteriesRejectsDuplicateTargets(t *testing.T) {
	t.Parallel()

	// Two IDs pointing at one battery would write every reading twice.
	_, err := ParseBatteries(map[string]string{
		"garage": "same-battery",
		"shed":   "same-battery",
	}, "")
	if err == nil {
		t.Fatal("expected an error for two batteries sharing a target")
	}
}

func TestParseBatteriesTrimsWhitespace(t *testing.T) {
	t.Parallel()

	batteries, err := ParseBatteries(map[string]string{"  garage  ": "  battery-one  "}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if batteries[0].ID != "garage" || batteries[0].Value != "battery-one" {
		t.Errorf("got %+v, want whitespace trimmed", batteries[0])
	}
}

func TestParseBatteriesRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		entries  map[string]string
		fallback string
	}{
		{name: "nothing configured at all", entries: nil, fallback: ""},
		{name: "empty id", entries: map[string]string{"": "battery-one"}, fallback: ""},
		{name: "whitespace only id", entries: map[string]string{"   ": "battery-one"}, fallback: ""},
		{name: "empty value", entries: map[string]string{"garage": ""}, fallback: ""},
		{name: "whitespace only value", entries: map[string]string{"garage": "   "}, fallback: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseBatteries(tt.entries, tt.fallback); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

// A value that is not a device address must be treated as an advertised name on
// every platform, since that is the only other thing it can be.
func TestParseBatteriesClassifiesNonAddressAsName(t *testing.T) {
	t.Parallel()

	batteries, err := ParseBatteries(map[string]string{"garage": "L-12100BNNA70-A19081"}, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if batteries[0].Match != MatchName {
		t.Errorf("got match %q, want %q", batteries[0].Match, MatchName)
	}
}
