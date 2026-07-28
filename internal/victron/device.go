package victron

import (
	"fmt"
	"sort"
	"strings"

	litimebluetooth "alpineworks.io/go-litime-bluetooth/bluetooth"
	victronble "alpineworks.io/go-victron-ble"
)

// Device is one configured Victron device to read.
type Device struct {
	// ID is the stable identifier recorded alongside every reading. It is
	// operator-chosen, so replacing hardware does not rewrite history under a
	// different key.
	ID string

	// Address is the BLE device address, upper-cased for comparison against
	// scan results.
	Address string

	// Key is the 16 byte advertisement key from VictronConnect.
	Key []byte
}

// ParseDevices builds the device list from configuration.
//
// addresses maps an operator-chosen ID to a device address; keys maps the same
// IDs to their advertisement keys. Both are required for every device: an
// address without a key cannot be decrypted, and a key without an address
// belongs to a device that is not configured, which is more likely a typo than
// an intention.
func ParseDevices(addresses, keys map[string]string) ([]Device, error) {
	if len(addresses) == 0 {
		return nil, nil
	}

	devices := make([]Device, 0, len(addresses))
	seen := make(map[string]string, len(addresses))

	for id, address := range addresses {
		id = strings.TrimSpace(id)
		address = strings.TrimSpace(address)

		if id == "" {
			return nil, fmt.Errorf("victron device id must not be empty")
		}
		if address == "" {
			return nil, fmt.Errorf("victron device %q has no address", id)
		}

		// Parsing here rather than at connect time means a mistyped address is
		// a startup failure instead of a device that is simply never seen.
		parsed, err := litimebluetooth.ParseAddress(address)
		if err != nil {
			return nil, fmt.Errorf("victron device %q: %w", id, err)
		}

		rawKey, ok := keys[id]
		if !ok {
			return nil, fmt.Errorf("victron device %q has no encryption key; set one in VICTRON_KEYS", id)
		}

		key, err := victronble.ParseKey(strings.TrimSpace(rawKey))
		if err != nil {
			return nil, fmt.Errorf("victron device %q: %w", id, err)
		}

		normalised := strings.ToUpper(parsed.String())
		if previous, duplicate := seen[normalised]; duplicate {
			return nil, fmt.Errorf("victron devices %q and %q both refer to %s", previous, id, normalised)
		}
		seen[normalised] = id

		devices = append(devices, Device{ID: id, Address: normalised, Key: key})
	}

	// A key naming a device that is not configured is almost always a typo in
	// one of the two variables, and silently ignoring it would leave the device
	// unread with no indication why.
	for id := range keys {
		if _, ok := addresses[strings.TrimSpace(id)]; !ok {
			return nil, fmt.Errorf("victron key %q has no matching entry in VICTRON_DEVICES", id)
		}
	}

	// Map iteration is randomised, so sort for stable logs and startup order.
	sort.Slice(devices, func(i, j int) bool { return devices[i].ID < devices[j].ID })

	return devices, nil
}
