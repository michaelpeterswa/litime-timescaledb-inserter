//go:build linux

package litime

import (
	tinygobluetooth "tinygo.org/x/bluetooth"
)

// resolveAdapter returns the Bluetooth adapter to use. An empty name selects
// the default adapter, which is hci0.
//
// Naming an adapter matters when the host has more than one radio. The Pi's
// onboard chip shares a single antenna between WiFi and Bluetooth, and a busy
// 2.4GHz WiFi link starves Bluetooth badly enough that connections are
// established and then immediately dropped. Moving Bluetooth to a USB dongle
// gives it its own radio, and that dongle comes up as hci1.
func resolveAdapter(name string) (*tinygobluetooth.Adapter, error) {
	if name == "" {
		return tinygobluetooth.DefaultAdapter, nil
	}

	return tinygobluetooth.NewAdapter(name), nil
}
