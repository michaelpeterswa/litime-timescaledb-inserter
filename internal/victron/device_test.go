package victron

import "testing"

const (
	// A syntactically valid 16 byte advertisement key. Not a real device key.
	testKey      = "000102030405060708090a0b0c0d0e0f"
	otherTestKey = "0f0e0d0c0b0a09080706050403020100"
)

func TestParseDevicesEmptyIsNotAnError(t *testing.T) {
	t.Parallel()

	// Victron support is optional, so no configuration means no devices rather
	// than a failure to start.
	devices, err := ParseDevices(nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(devices) != 0 {
		t.Errorf("got %d devices, want none", len(devices))
	}
}

func TestParseDevicesRequiresAKeyPerDevice(t *testing.T) {
	t.Parallel()

	// A device with no key can be seen but never decrypted, which would look
	// like a device that is present but silent.
	_, err := ParseDevices(
		map[string]string{"smartsolar": "11:22:33:AA:BB:CC"},
		nil,
	)
	if err == nil {
		t.Fatal("expected an error when a device has no encryption key")
	}
}

func TestParseDevicesRejectsOrphanedKey(t *testing.T) {
	t.Parallel()

	// A key naming a device that is not configured is almost always a typo in
	// one of the two variables. Ignoring it would leave a device unread with no
	// indication why.
	_, err := ParseDevices(
		map[string]string{"smartsolar": "11:22:33:AA:BB:CC"},
		map[string]string{"smartsolar": testKey, "typo": otherTestKey},
	)
	if err == nil {
		t.Fatal("expected an error for a key with no matching device")
	}
}

func TestParseDevicesRejectsBadKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		key  string
	}{
		{name: "not hex", key: "not-a-key"},
		{name: "too short", key: "0011223344"},
		{name: "too long", key: testKey + "00"},
		{name: "empty", key: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			_, err := ParseDevices(
				map[string]string{"smartsolar": "11:22:33:AA:BB:CC"},
				map[string]string{"smartsolar": tt.key},
			)
			if err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestParseDevicesRejectsBadInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		addresses map[string]string
		keys      map[string]string
	}{
		{
			name:      "empty id",
			addresses: map[string]string{"": "11:22:33:AA:BB:CC"},
			keys:      map[string]string{"": testKey},
		},
		{
			name:      "empty address",
			addresses: map[string]string{"smartsolar": ""},
			keys:      map[string]string{"smartsolar": testKey},
		},
		{
			name:      "unparseable address",
			addresses: map[string]string{"smartsolar": "not-an-address"},
			keys:      map[string]string{"smartsolar": testKey},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := ParseDevices(tt.addresses, tt.keys); err == nil {
				t.Error("expected an error, got nil")
			}
		})
	}
}

func TestNewCollectorRejectsScanLongerThanInterval(t *testing.T) {
	t.Parallel()

	// A scan holds the radio, so one lasting as long as the interval would
	// leave it permanently busy and starve battery reconnects.
	_, err := NewCollector(CollectorOptions{
		Devices:      []Device{{ID: "smartsolar", Address: "11:22:33:AA:BB:CC", Key: make([]byte, 16)}},
		ScanInterval: 5 * 1e9,
		ScanDuration: 5 * 1e9,
	})
	if err == nil {
		t.Fatal("expected an error when the scan duration is not shorter than the interval")
	}
}

func TestNewCollectorRequiresDevicesAndAdapter(t *testing.T) {
	t.Parallel()

	if _, err := NewCollector(CollectorOptions{}); err == nil {
		t.Error("expected an error with no devices configured")
	}

	// An adapter is required rather than defaulted, so that this collector
	// cannot silently use a different radio than the rest of the process.
	_, err := NewCollector(CollectorOptions{
		Devices: []Device{{ID: "smartsolar", Address: "11:22:33:AA:BB:CC", Key: make([]byte, 16)}},
	})
	if err == nil {
		t.Error("expected an error with no adapter provided")
	}
}
