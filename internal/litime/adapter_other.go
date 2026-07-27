//go:build !linux

package litime

import (
	"fmt"

	tinygobluetooth "tinygo.org/x/bluetooth"
)

// resolveAdapter returns the Bluetooth adapter to use.
//
// Only Linux exposes adapters by name; elsewhere the underlying stack offers a
// single default adapter and nothing to select between. Naming one is rejected
// rather than ignored, so a configuration that cannot be honoured fails at
// startup instead of quietly using the wrong radio.
func resolveAdapter(name string) (*tinygobluetooth.Adapter, error) {
	if name != "" {
		return nil, fmt.Errorf("selecting a bluetooth adapter by name is only supported on linux (got %q)", name)
	}

	return tinygobluetooth.DefaultAdapter, nil
}
