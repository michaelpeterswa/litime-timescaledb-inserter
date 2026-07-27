package litime

import (
	"fmt"
	"sort"
	"strings"

	"alpineworks.io/go-litime-bluetooth/bluetooth"
)

// MatchKind is how a battery is located over Bluetooth.
type MatchKind string

const (
	// MatchAddress locates a battery by its BLE device address. This is exact
	// and is the only reliable option when batteries share an advertised name.
	MatchAddress MatchKind = "address"

	// MatchName locates a battery by its advertised BLE local name. It only
	// works when the name is unique among the batteries in range.
	MatchName MatchKind = "name"
)

// Battery is one configured battery to poll.
type Battery struct {
	// ID is the stable identifier recorded alongside every measurement. It is
	// operator-chosen, so renaming or replacing hardware does not rewrite
	// history under a different key.
	ID string

	// Match is how Value is interpreted.
	Match MatchKind

	// Value is a device address or an advertised local name, per Match.
	Value string
}

// ParseBatteries builds the battery list from configuration.
//
// entries maps an operator-chosen ID to either a device address or an advertised
// local name; which one is decided by whether the value parses as an address for
// this platform. Callers should log the resulting Match so a mistyped address
// falling back to a name lookup is visible rather than silent.
//
// fallbackName preserves the older single-battery configuration and is used only
// when entries is empty.
func ParseBatteries(entries map[string]string, fallbackName string) ([]Battery, error) {
	if len(entries) == 0 {
		if fallbackName == "" {
			return nil, fmt.Errorf("no batteries configured")
		}

		return []Battery{{ID: fallbackName, Match: MatchName, Value: fallbackName}}, nil
	}

	batteries := make([]Battery, 0, len(entries))
	seen := make(map[string]string, len(entries))

	for id, value := range entries {
		id = strings.TrimSpace(id)
		value = strings.TrimSpace(value)

		if id == "" {
			return nil, fmt.Errorf("battery id must not be empty")
		}
		if value == "" {
			return nil, fmt.Errorf("battery %q has no address or name", id)
		}

		match := MatchName
		if _, err := bluetooth.ParseAddress(value); err == nil {
			match = MatchAddress
		}

		// Two IDs pointing at one battery would double every measurement and
		// silently halve the apparent poll interval for both.
		if previous, duplicate := seen[value]; duplicate {
			return nil, fmt.Errorf("batteries %q and %q both refer to %q", previous, id, value)
		}
		seen[value] = id

		batteries = append(batteries, Battery{ID: id, Match: match, Value: value})
	}

	// Map iteration is randomised, so sort for stable logs and startup order.
	sort.Slice(batteries, func(i, j int) bool { return batteries[i].ID < batteries[j].ID })

	return batteries, nil
}
