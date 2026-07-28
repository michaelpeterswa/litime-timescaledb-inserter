package victron

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	litimebluetooth "alpineworks.io/go-litime-bluetooth/bluetooth"
	victronble "alpineworks.io/go-victron-ble"
	tinygobluetooth "tinygo.org/x/bluetooth"
)

// Reading is one decoded solar charger advertisement.
type Reading struct {
	DeviceID   string
	ObservedAt time.Time
	ModelID    uint16
	ModelName  string
	RecordType uint8
	Data       *victronble.SolarCharger
}

// CollectorOptions configures a Collector.
type CollectorOptions struct {
	Devices []Device

	// ScanInterval is how often a scan runs. Each scan holds the radio, so
	// device connections elsewhere in the process queue behind it; a short scan
	// on a slow interval keeps that window small.
	ScanInterval time.Duration

	// ScanDuration bounds a single scan.
	ScanDuration time.Duration

	// BufferSize bounds the reading queue handed to the consumer.
	BufferSize int

	Adapter *tinygobluetooth.Adapter
	Logger  *slog.Logger
	Metrics *Metrics
}

// Collector reads Victron devices by observing their advertisements.
//
// Victron broadcasts state in the advertisement rather than exposing it over a
// connection, so this never connects to anything: it scans, decrypts what it
// saw, and publishes. Scans go through the Bluetooth library so they are
// serialised against connections made elsewhere on the same adapter, which is
// the whole reason this lives in the same process as the battery collector
// rather than beside it.
type Collector struct {
	devices      []Device
	byAddress    map[string]Device
	scanInterval time.Duration
	scanDuration time.Duration
	adapter      *tinygobluetooth.Adapter
	logger       *slog.Logger
	metrics      *Metrics

	readings chan Reading
	// closeMu guards closing readings. Nothing publishes after Run returns, but
	// the guard keeps that a property of the code rather than of the ordering.
	closeMu sync.RWMutex
	closed  bool
}

func NewCollector(opts CollectorOptions) (*Collector, error) {
	if len(opts.Devices) == 0 {
		return nil, fmt.Errorf("no victron devices configured")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = 64
	}
	if opts.ScanInterval <= 0 {
		opts.ScanInterval = time.Minute
	}
	if opts.ScanDuration <= 0 {
		opts.ScanDuration = 5 * time.Second
	}
	if opts.ScanDuration >= opts.ScanInterval {
		return nil, fmt.Errorf("scan duration (%s) must be shorter than the scan interval (%s), or the radio is never free", opts.ScanDuration, opts.ScanInterval)
	}
	if opts.Adapter == nil {
		return nil, fmt.Errorf("no bluetooth adapter provided")
	}

	byAddress := make(map[string]Device, len(opts.Devices))
	for _, device := range opts.Devices {
		byAddress[device.Address] = device
	}

	return &Collector{
		devices:      opts.Devices,
		byAddress:    byAddress,
		scanInterval: opts.ScanInterval,
		scanDuration: opts.ScanDuration,
		adapter:      opts.Adapter,
		logger:       opts.Logger,
		metrics:      opts.Metrics,
		readings:     make(chan Reading, opts.BufferSize),
	}, nil
}

// Readings returns the channel decoded advertisements are published on. It is
// closed once Run has returned.
func (c *Collector) Readings() <-chan Reading {
	return c.readings
}

// Run scans on an interval until ctx is cancelled.
func (c *Collector) Run(ctx context.Context) error {
	defer c.closeReadings()

	// Scan immediately so a restart produces data without waiting out a full
	// interval.
	c.scanOnce(ctx)

	ticker := time.NewTicker(c.scanInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			c.scanOnce(ctx)
		}
	}
}

func (c *Collector) scanOnce(ctx context.Context) {
	addresses := make([]tinygobluetooth.Address, 0, len(c.devices))
	for _, device := range c.devices {
		address, err := litimebluetooth.ParseAddress(device.Address)
		if err != nil {
			// Addresses were validated at startup, so this cannot normally
			// happen; skipping beats aborting the whole scan.
			c.logger.Error("skipping victron device with unparseable address",
				slog.String("device_id", device.ID),
				slog.String("address", device.Address),
				slog.String("error", err.Error()))
			continue
		}
		addresses = append(addresses, address)
	}

	if len(addresses) == 0 {
		return
	}

	devices, err := litimebluetooth.ScanForDevices(ctx,
		litimebluetooth.ScanWithAdapter(c.adapter),
		litimebluetooth.ScanWithLogger(c.logger),
		litimebluetooth.ScanWithTimeout(c.scanDuration),
		litimebluetooth.ScanWithAddresses(addresses...),
	)
	if err != nil {
		if ctx.Err() != nil {
			return
		}
		c.logger.Error("victron scan failed", slog.String("error", err.Error()))
		c.metrics.recordScanFailure(ctx)
		return
	}

	seen := make(map[string]struct{}, len(devices))
	for _, found := range devices {
		device, ok := c.byAddress[strings.ToUpper(found.Address.String())]
		if !ok {
			continue
		}

		if c.handle(ctx, device, found) {
			seen[device.ID] = struct{}{}
		}
	}

	// A device that advertised nothing this round is worth surfacing: it is the
	// only signal that a passive collector has lost sight of something, since
	// there is no connection to drop.
	for _, device := range c.devices {
		if _, ok := seen[device.ID]; !ok {
			c.logger.Warn("no victron reading this scan", slog.String("device_id", device.ID))
			c.metrics.recordMissed(ctx, device.ID)
		}
	}
}

// handle decodes one scan result, reporting whether a reading was published.
func (c *Collector) handle(ctx context.Context, device Device, found litimebluetooth.DiscoveredDevice) bool {
	data, ok := found.ManufacturerData[victronble.CompanyID]
	if !ok {
		return false
	}

	record, err := victronble.ParseRecord(data)
	if err != nil {
		// Victron uses manufacturer data for records other than instant
		// readout, so this is routine rather than a fault.
		c.logger.Debug("skipping victron advertisement",
			slog.String("device_id", device.ID),
			slog.String("reason", err.Error()))
		return false
	}

	if record.Type != victronble.RecordSolarCharger {
		c.logger.Debug("ignoring unsupported victron record type",
			slog.String("device_id", device.ID),
			slog.String("record_type", record.Type.String()))
		return false
	}

	payload, err := victronble.Decrypt(record, device.Key)
	if err != nil {
		// A key mismatch is a configuration error that will not fix itself, so
		// it is worth more noise than a malformed frame.
		var mismatch *victronble.ErrKeyMismatch
		if errors.As(err, &mismatch) {
			c.logger.Error("victron encryption key does not match this device",
				slog.String("device_id", device.ID),
				slog.String("error", err.Error()))
		} else {
			c.logger.Warn("failed to decrypt victron advertisement",
				slog.String("device_id", device.ID),
				slog.String("error", err.Error()))
		}
		c.metrics.recordDecryptFailure(ctx, device.ID)
		return false
	}

	charger, err := victronble.ParseSolarCharger(payload)
	if err != nil {
		c.logger.Warn("failed to parse victron payload",
			slog.String("device_id", device.ID),
			slog.String("error", err.Error()))
		c.metrics.recordDecodeFailure(ctx, device.ID)
		return false
	}

	modelName, _ := victronble.ModelName(record.ModelID)

	reading := Reading{
		DeviceID:   device.ID,
		ObservedAt: time.Now(),
		ModelID:    record.ModelID,
		ModelName:  modelName,
		RecordType: uint8(record.Type),
		Data:       charger,
	}

	if !c.publish(reading) {
		c.logger.Warn("victron reading queue full, dropping reading", slog.String("device_id", device.ID))
		c.metrics.recordDropped(ctx, device.ID)
		return true
	}

	c.metrics.recordReading(ctx, device.ID)

	return true
}

// publish offers a reading without blocking the scan loop.
func (c *Collector) publish(reading Reading) bool {
	c.closeMu.RLock()
	defer c.closeMu.RUnlock()

	if c.closed {
		return false
	}

	select {
	case c.readings <- reading:
		return true
	default:
		return false
	}
}

func (c *Collector) closeReadings() {
	c.closeMu.Lock()
	defer c.closeMu.Unlock()

	c.closed = true
	close(c.readings)
}
