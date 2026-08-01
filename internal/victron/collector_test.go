package victron

import (
	"context"
	"crypto/aes"
	"encoding/binary"
	"encoding/hex"
	"io"
	"log/slog"
	"testing"

	litimebluetooth "alpineworks.io/go-litime-bluetooth/bluetooth"
	victronble "alpineworks.io/go-victron-ble"
)

// handle's return value feeds the missed counter, and the two questions it sits
// between are easy to conflate: "is this device transmitting" and "did we get a
// reading out of it". These tests pin the difference, because getting it wrong
// is silent -- a working device reported as absent, or a device with instant
// readout switched off reported as healthy.

func TestHandleBatteryMonitorPublishesReading(t *testing.T) {
	t.Parallel()

	collector, device := testCollector(t)

	// A real capture from a SmartShunt IP65 300A: 26.60 V, midpoint aux.
	payload := decodeHex(t, "ffff640a0000320531fcffffffffff")
	found := advertisement(t, device, 0xC039, victronble.RecordBatteryMonitor, payload)

	if seen := collector.handle(context.Background(), device, found); !seen {
		t.Fatal("handle reported the device as not seen")
	}

	select {
	case reading := <-collector.readings:
		if reading.BatteryMonitor == nil {
			t.Fatal("reading carried no battery monitor payload")
		}
		if reading.SolarCharger != nil {
			t.Error("reading carried a solar charger payload as well")
		}
		if reading.ModelName != "SmartShunt IP65 300A/50mV" {
			t.Errorf("model name = %q", reading.ModelName)
		}
		if v := reading.BatteryMonitor.BatteryVoltage; v == nil || *v != 26.60 {
			t.Errorf("battery voltage = %v, want 26.60", v)
		}
		if reading.BatteryMonitor.AuxInputType != victronble.AuxInputMidpointVoltage {
			t.Errorf("aux input type = %v, want midpoint", reading.BatteryMonitor.AuxInputType)
		}
	default:
		t.Fatal("nothing was published")
	}
}

// A record type with no parser still means the device is alive and
// transmitting. Counting it as missed sends whoever reads the metric looking
// for a radio problem that does not exist.
func TestHandleUnsupportedRecordTypeCountsAsSeen(t *testing.T) {
	t.Parallel()

	collector, device := testCollector(t)

	found := advertisement(t, device, 0xA381, victronble.RecordInverter, make([]byte, 12))

	if seen := collector.handle(context.Background(), device, found); !seen {
		t.Error("an unsupported record type was reported as a missing device")
	}

	select {
	case reading := <-collector.readings:
		t.Fatalf("published a reading for an undecodable record type: %+v", reading)
	default:
	}
}

// The opposite case, and the reason the distinction is not simply "did we see
// any Victron bytes": a device with instant readout switched off advertises a
// four byte header carrying its model and nothing else. No data will ever come
// from it, so it must read as missing rather than as healthy.
func TestHandleWithoutInstantReadoutCountsAsMissed(t *testing.T) {
	t.Parallel()

	collector, device := testCollector(t)

	// Captured from the SmartShunt before instant readout was enabled.
	found := litimebluetooth.DiscoveredDevice{
		ManufacturerData: map[uint16][]byte{victronble.CompanyID: decodeHex(t, "100239c0")},
	}

	if seen := collector.handle(context.Background(), device, found); seen {
		t.Error("a device advertising no instant readout was reported as seen")
	}
}

func TestHandleIgnoresNonVictronAdvertisement(t *testing.T) {
	t.Parallel()

	collector, device := testCollector(t)

	found := litimebluetooth.DiscoveredDevice{
		ManufacturerData: map[uint16][]byte{0x004C: {1, 2, 3}},
	}

	if seen := collector.handle(context.Background(), device, found); seen {
		t.Error("a non-Victron advertisement was reported as a Victron sighting")
	}
}

func testCollector(t *testing.T) (*Collector, Device) {
	t.Helper()

	key, err := victronble.ParseKey(testKey)
	if err != nil {
		t.Fatalf("ParseKey: %v", err)
	}

	device := Device{ID: "shunt", Address: "F1:8C:29:05:9D:BA", Key: key}

	collector := &Collector{
		devices:   []Device{device},
		byAddress: map[string]Device{device.Address: device},
		logger:    slog.New(slog.NewTextHandler(io.Discard, nil)),
		readings:  make(chan Reading, 1),
	}

	return collector, device
}

// advertisement builds the manufacturer data a device would broadcast for the
// given payload, encrypting it the way the device does so the test exercises
// the real decrypt path rather than bypassing it.
func advertisement(t *testing.T, device Device, modelID uint16, recordType victronble.RecordType, payload []byte) litimebluetooth.DiscoveredDevice {
	t.Helper()

	const iv uint16 = 0x0123

	block, err := aes.NewCipher(device.Key)
	if err != nil {
		t.Fatalf("cipher: %v", err)
	}

	counter := make([]byte, aes.BlockSize)
	binary.LittleEndian.PutUint16(counter, iv)

	keystream := make([]byte, aes.BlockSize)
	block.Encrypt(keystream, counter)

	if len(payload) > aes.BlockSize {
		t.Fatalf("test payload of %d bytes spans more than one AES block", len(payload))
	}

	data := make([]byte, 8, 8+len(payload))
	binary.LittleEndian.PutUint16(data[0:], 0x0210)
	binary.LittleEndian.PutUint16(data[2:], modelID)
	data[4] = byte(recordType)
	binary.LittleEndian.PutUint16(data[5:], iv)
	data[7] = device.Key[0]

	for i, b := range payload {
		data = append(data, b^keystream[i])
	}

	return litimebluetooth.DiscoveredDevice{
		ManufacturerData: map[uint16][]byte{victronble.CompanyID: data},
	}
}

func decodeHex(t *testing.T, s string) []byte {
	t.Helper()

	data, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("bad fixture %q: %v", s, err)
	}

	return data
}
