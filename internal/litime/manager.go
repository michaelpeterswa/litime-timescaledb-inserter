package litime

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	golitimebluetooth "alpineworks.io/go-litime-bluetooth"
	"alpineworks.io/go-litime-bluetooth/bluetooth"
	tinygobluetooth "tinygo.org/x/bluetooth"
)

const (
	minReconnectBackoff = 5 * time.Second
	maxReconnectBackoff = 5 * time.Minute
)

// Reading is one measurement together with the battery that produced it.
type Reading struct {
	BatteryID  string
	ObservedAt time.Time
	Data       *golitimebluetooth.LiTimeBatteryData
}

// ManagerOptions configures a Manager.
type ManagerOptions struct {
	Batteries      []Battery
	ScrapeInterval time.Duration
	ScanTimeout    time.Duration

	// StaleTimeout is how long a battery may stay silent before it is treated
	// as disconnected and cycled.
	StaleTimeout time.Duration

	// BufferSize bounds the reading queue handed to the consumer.
	BufferSize int

	// AdapterName selects the Bluetooth adapter, for example "hci1". Empty
	// means the default adapter. Only meaningful on Linux.
	AdapterName string

	Logger  *slog.Logger
	Metrics *Metrics
}

// Manager keeps a connection to every configured battery and publishes their
// readings on a single channel.
//
// One Manager owns all the batteries deliberately. They share a radio, and the
// Bluetooth stack allows only one scan at a time, so resolving addresses has to
// happen centrally rather than per battery.
type Manager struct {
	batteries      []Battery
	scrapeInterval time.Duration
	scanTimeout    time.Duration
	staleTimeout   time.Duration
	adapter        *tinygobluetooth.Adapter
	logger         *slog.Logger
	metrics        *Metrics

	readings chan Reading
	// closeMu guards closing readings against concurrent publishes. The
	// Bluetooth stack's notification goroutine outlives Disconnect, so without
	// this a notification arriving during shutdown would send on a closed
	// channel and panic.
	closeMu sync.RWMutex
	closed  bool
}

// publishResult reports what became of a reading offered to the queue.
type publishResult int

const (
	published publishResult = iota
	droppedQueueFull
	droppedShuttingDown
)

func NewManager(opts ManagerOptions) (*Manager, error) {
	if len(opts.Batteries) == 0 {
		return nil, fmt.Errorf("no batteries configured")
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.BufferSize <= 0 {
		opts.BufferSize = 256
	}
	if opts.StaleTimeout <= 0 {
		opts.StaleTimeout = 90 * time.Second
	}
	if opts.ScrapeInterval <= 0 {
		opts.ScrapeInterval = 10 * time.Second
	}
	if opts.StaleTimeout <= opts.ScrapeInterval {
		return nil, fmt.Errorf("stale timeout (%s) must exceed scrape interval (%s)", opts.StaleTimeout, opts.ScrapeInterval)
	}

	adapter, err := resolveAdapter(opts.AdapterName)
	if err != nil {
		return nil, err
	}

	return &Manager{
		batteries:      opts.Batteries,
		scrapeInterval: opts.ScrapeInterval,
		scanTimeout:    opts.ScanTimeout,
		staleTimeout:   opts.StaleTimeout,
		adapter:        adapter,
		logger:         opts.Logger,
		metrics:        opts.Metrics,
		readings:       make(chan Reading, opts.BufferSize),
	}, nil
}

// Readings returns the channel every battery publishes to. It is closed once Run
// has returned and no further readings will arrive.
func (m *Manager) Readings() <-chan Reading {
	return m.readings
}

// Run supervises every battery until ctx is cancelled.
//
// Batteries that cannot be reached do not prevent the others from running: each
// is supervised independently and retried with backoff, so one flat or
// out-of-range battery never stalls the rest.
func (m *Manager) Run(ctx context.Context) error {
	defer m.closeReadings()

	resolved, err := m.resolveAddresses(ctx)
	if err != nil {
		return err
	}

	var wg sync.WaitGroup
	for _, battery := range m.batteries {
		address, ok := resolved[battery.ID]
		if !ok {
			// Only name-matched batteries can be missing here, and a battery
			// absent at startup may well appear later, so keep supervising it.
			m.logger.Warn("battery not found during scan, will keep retrying",
				slog.String("battery_id", battery.ID),
				slog.String("name", battery.Value))
		}

		wg.Add(1)
		go func() {
			defer wg.Done()
			m.superviseBattery(ctx, battery, address, ok)
		}()
	}

	wg.Wait()

	return nil
}

// resolveAddresses turns the configured batteries into device addresses.
//
// Everything matched by name is resolved in a single scan. Scanning per battery
// is not an option: the adapter runs one scan at a time, so concurrent scans
// fail and sequential ones pay the timeout once per battery.
func (m *Manager) resolveAddresses(ctx context.Context) (map[string]tinygobluetooth.Address, error) {
	resolved := make(map[string]tinygobluetooth.Address, len(m.batteries))

	var names []string
	byName := make(map[string][]string)

	for _, battery := range m.batteries {
		if battery.Match == MatchAddress {
			address, err := bluetooth.ParseAddress(battery.Value)
			if err != nil {
				return nil, fmt.Errorf("battery %q: %w", battery.ID, err)
			}
			resolved[battery.ID] = address
			continue
		}

		if _, seen := byName[battery.Value]; !seen {
			names = append(names, battery.Value)
		}
		byName[battery.Value] = append(byName[battery.Value], battery.ID)
	}

	if len(names) == 0 {
		return resolved, nil
	}

	m.logger.Info("scanning for batteries by name", slog.Any("names", names))

	devices, err := bluetooth.ScanForDevices(ctx,
		bluetooth.ScanWithAdapter(m.adapter),
		bluetooth.ScanWithLogger(m.logger),
		bluetooth.ScanWithTimeout(m.scanTimeout),
		bluetooth.ScanWithNames(names...),
		bluetooth.ScanWithTargetCount(len(names)),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to scan for batteries: %w", err)
	}

	found := make(map[string][]tinygobluetooth.Address)
	for _, device := range devices {
		found[device.Name] = append(found[device.Name], device.Address)
	}

	for name, ids := range byName {
		matches := found[name]

		// Several batteries answering to one name cannot be told apart, and
		// picking arbitrarily would attribute readings to the wrong battery.
		if len(matches) > 1 {
			return nil, fmt.Errorf(
				"%d devices advertise the name %q; configure these batteries by address instead: %v",
				len(matches), name, addressStrings(matches))
		}

		if len(matches) == 0 {
			continue
		}

		for _, id := range ids {
			resolved[id] = matches[0]
		}
	}

	return resolved, nil
}

func addressStrings(addresses []tinygobluetooth.Address) []string {
	out := make([]string, 0, len(addresses))
	for _, address := range addresses {
		out = append(out, address.String())
	}

	return out
}

// superviseBattery keeps one battery connected and polled for the lifetime of
// ctx, reconnecting whenever it falls silent.
func (m *Manager) superviseBattery(ctx context.Context, battery Battery, address tinygobluetooth.Address, haveAddress bool) {
	logger := m.logger.With(slog.String("battery_id", battery.ID))
	backoff := minReconnectBackoff

	for ctx.Err() == nil {
		if !haveAddress {
			// The battery was not in range at startup. Rescanning is cheap
			// relative to the retry interval and is the only way it can appear.
			found, err := m.resolveAddresses(ctx)
			if err != nil {
				logger.Error("failed to scan for battery", slog.String("error", err.Error()))
			} else if a, ok := found[battery.ID]; ok {
				address, haveAddress = a, true
			}

			if !haveAddress {
				if !sleep(ctx, backoff) {
					return
				}
				backoff = nextBackoff(backoff)
				continue
			}
		}

		client, lastSeen, err := m.connect(ctx, battery, address, logger)
		if err != nil {
			logger.Error("failed to connect to battery",
				slog.String("address", address.String()),
				slog.String("error", err.Error()))
			m.metrics.recordConnected(ctx, battery.ID, false)

			if !sleep(ctx, backoff) {
				return
			}
			backoff = nextBackoff(backoff)
			continue
		}

		logger.Info("connected to battery", slog.String("address", address.String()))
		m.metrics.recordConnected(ctx, battery.ID, true)
		backoff = minReconnectBackoff

		reason := m.pollUntilStale(ctx, client, lastSeen)

		if err := client.Disconnect(); err != nil {
			logger.Warn("failed to disconnect cleanly", slog.String("error", err.Error()))
		}
		m.metrics.recordConnected(ctx, battery.ID, false)

		if ctx.Err() != nil {
			return
		}

		logger.Warn("battery connection lost, reconnecting", slog.String("reason", reason))
		m.metrics.recordReconnect(ctx, battery.ID)

		if !sleep(ctx, backoff) {
			return
		}
		backoff = nextBackoff(backoff)
	}
}

// connect builds a client for one battery and establishes its connection. The
// returned counter carries the time of that connection's most recent reading.
func (m *Manager) connect(ctx context.Context, battery Battery, address tinygobluetooth.Address, logger *slog.Logger) (*bluetooth.LiTimeBluetoothClient, *atomic.Int64, error) {
	// lastSeen belongs to this connection attempt alone, so a reading from a
	// previous connection cannot make a fresh one look alive.
	lastSeen := &atomic.Int64{}
	lastSeen.Store(time.Now().UnixNano())

	client := bluetooth.NewLiTimeBluetoothClient(battery.Value,
		bluetooth.WithAddress(address),
		bluetooth.WithAdapter(m.adapter),
		bluetooth.WithLogger(logger),
		bluetooth.WithScanTimeout(m.scanTimeout),
		bluetooth.WithEnableNotificationCallback(func(b []byte) {
			m.handleNotification(ctx, battery, b, lastSeen, logger)
		}),
	)

	if err := client.Connect(ctx); err != nil {
		return nil, nil, err
	}

	return client, lastSeen, nil
}

// handleNotification parses an incoming notification and queues it.
//
// This runs on the Bluetooth stack's dispatch goroutine, so it must not block:
// the queue is therefore offered a reading and the reading is dropped if the
// consumer has fallen behind.
func (m *Manager) handleNotification(ctx context.Context, battery Battery, b []byte, lastSeen *atomic.Int64, logger *slog.Logger) {
	data, err := golitimebluetooth.ParseLiTimeBatteryData(b)
	if err != nil {
		logger.Error("failed to parse notification data", slog.String("error", err.Error()))
		m.metrics.recordParseFailure(ctx, battery.ID)
		return
	}

	lastSeen.Store(time.Now().UnixNano())
	m.metrics.recordNotification(ctx, battery.ID)

	switch m.publish(Reading{
		BatteryID:  battery.ID,
		ObservedAt: time.Now(),
		Data:       data,
	}) {
	case droppedQueueFull:
		logger.Warn("reading queue full, dropping reading")
		m.metrics.recordDropped(ctx, battery.ID)
	case droppedShuttingDown:
		logger.Debug("dropping reading received during shutdown")
	case published:
	}
}

// publish offers a reading to the queue without ever blocking, because it runs
// on the Bluetooth stack's dispatch goroutine.
func (m *Manager) publish(reading Reading) publishResult {
	m.closeMu.RLock()
	defer m.closeMu.RUnlock()

	if m.closed {
		return droppedShuttingDown
	}

	select {
	case m.readings <- reading:
		return published
	default:
		return droppedQueueFull
	}
}

// closeReadings ends the stream, waiting out any publish already in flight.
func (m *Manager) closeReadings() {
	m.closeMu.Lock()
	defer m.closeMu.Unlock()

	m.closed = true
	close(m.readings)
}

// pollUntilStale queries the battery on an interval and returns once it stops
// answering or ctx is cancelled, reporting why.
func (m *Manager) pollUntilStale(ctx context.Context, client *bluetooth.LiTimeBluetoothClient, lastSeen *atomic.Int64) string {
	ticker := time.NewTicker(m.scrapeInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return "shutting down"
		case <-ticker.C:
			if err := client.QueryData(); err != nil {
				return fmt.Sprintf("query failed: %s", err)
			}

			// A central gets no notification when a peer disappears, so silence
			// is the only signal that the battery is gone.
			silence := time.Since(time.Unix(0, lastSeen.Load()))
			if silence > m.staleTimeout {
				return fmt.Sprintf("no reading for %s", silence.Round(time.Second))
			}
		}
	}
}

// sleep waits for d, reporting false if ctx was cancelled first.
func sleep(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > maxReconnectBackoff {
		return maxReconnectBackoff
	}

	return next
}
