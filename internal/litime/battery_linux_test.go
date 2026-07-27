//go:build linux

package litime

import "testing"

// Device addresses are MAC addresses on Linux, so address-versus-name
// classification can only be asserted per platform.
func TestParseBatteriesClassifiesAddresses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  MatchKind
	}{
		{name: "upper case mac", value: "11:22:33:AA:BB:CC", want: MatchAddress},
		{name: "lower case mac", value: "11:22:33:aa:bb:cc", want: MatchAddress},
		{name: "advertised name", value: "LiTime Battery", want: MatchName},
		{name: "serial style name", value: "L-12100BNNA70-A19081", want: MatchName},
		// A truncated MAC is far more likely to be a typo than a device name,
		// but it cannot be told apart from one, so it is treated as a name and
		// the startup log is what surfaces the mistake.
		{name: "truncated mac falls back to name", value: "11:22:33:AA:BB", want: MatchName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			batteries, err := ParseBatteries(map[string]string{"garage": tt.value}, "")
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if batteries[0].Match != tt.want {
				t.Errorf("ParseBatteries(%q) matched by %q, want %q", tt.value, batteries[0].Match, tt.want)
			}
		})
	}
}
