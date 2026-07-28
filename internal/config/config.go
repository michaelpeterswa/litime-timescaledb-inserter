package config

import (
	"fmt"
	"time"

	"github.com/caarlos0/env/v11"
)

type Config struct {
	LogLevel string `env:"LOG_LEVEL" envDefault:"error"`

	// LitimeBatteries maps an operator-chosen battery ID to either a Bluetooth
	// device address or an advertised local name, for example:
	//
	//	LITIME_BATTERIES="garage=11:22:33:AA:BB:CC,shed=11:22:33:AA:BB:CD"
	//
	// The key/value separator is "=" rather than the library default of ":",
	// because MAC addresses are full of colons.
	LitimeBatteries map[string]string `env:"LITIME_BATTERIES" envKeyValSeparator:"="`

	// LitimeBatteryBluetoothName configures a single battery by name. It
	// predates LITIME_BATTERIES and is used only when that is unset.
	LitimeBatteryBluetoothName string `env:"LITIME_BATTERY_BLUETOOTH_NAME"`

	// VictronDevices maps an operator-chosen device ID to a Bluetooth device
	// address, for example:
	//
	//	VICTRON_DEVICES="smartsolar=F9:E3:C4:6E:85:D9"
	//
	// Leave unset to disable Victron collection entirely.
	VictronDevices map[string]string `env:"VICTRON_DEVICES" envKeyValSeparator:"="`

	// VictronKeys maps those same device IDs to their advertisement keys, as
	// shown by VictronConnect under Product Info, Instant Readout Details.
	//
	// These are credentials and belong in a secret alongside the database
	// password. Note that a device's key changes if its Bluetooth PIN is reset.
	VictronKeys map[string]string `env:"VICTRON_KEYS" envKeyValSeparator:"="`

	// VictronScanInterval is how often Victron devices are scanned for. Each
	// scan holds the radio, so battery reconnects queue behind it; the default
	// is deliberately slow relative to the scan duration.
	VictronScanInterval time.Duration `env:"VICTRON_SCAN_INTERVAL" envDefault:"60s"`

	// VictronScanDuration bounds a single Victron scan. It must be shorter than
	// the interval, or the radio is never free for anything else.
	VictronScanDuration time.Duration `env:"VICTRON_SCAN_DURATION" envDefault:"5s"`

	// BluetoothAdapter selects the adapter to use, for example "hci1". Empty
	// uses the default, which is hci0.
	//
	// The Raspberry Pi's onboard chip shares one antenna between WiFi and
	// Bluetooth, and a busy 2.4GHz WiFi link starves Bluetooth badly enough
	// that connections are established and then dropped immediately. Moving to
	// a USB dongle gives Bluetooth its own radio; the dongle comes up as hci1.
	BluetoothAdapter string `env:"BLUETOOTH_ADAPTER"`

	TimescaleConnString string        `env:"TIMESCALE_CONN_STRING,required"`
	ScrapeInterval      time.Duration `env:"SCRAPE_INTERVAL" envDefault:"10s"`
	CallbackTimeout     time.Duration `env:"CALLBACK_TIMEOUT" envDefault:"5s"`
	ScanTimeout         time.Duration `env:"SCAN_TIMEOUT" envDefault:"30s"`

	// StaleTimeout is how long a battery may go without returning a reading
	// before it is treated as gone and reconnected. As a central there is no
	// notification when a peer disappears, so silence is the only liveness
	// signal available. Keep it comfortably above SCRAPE_INTERVAL so an
	// occasional missed reply does not force a reconnect.
	StaleTimeout time.Duration `env:"STALE_TIMEOUT" envDefault:"90s"`

	// ReadingBufferSize bounds the queue between the Bluetooth callbacks and
	// the database writer. Readings are dropped rather than queued without
	// limit if the database cannot keep up.
	ReadingBufferSize int `env:"READING_BUFFER_SIZE" envDefault:"256"`

	MetricsEnabled bool `env:"METRICS_ENABLED" envDefault:"true"`
	MetricsPort    int  `env:"METRICS_PORT" envDefault:"8081"`

	Local bool `env:"LOCAL" envDefault:"false"`

	TracingEnabled    bool    `env:"TRACING_ENABLED" envDefault:"false"`
	TracingSampleRate float64 `env:"TRACING_SAMPLERATE" envDefault:"0.01"`
	TracingService    string  `env:"TRACING_SERVICE" envDefault:"litime-timescaledb-inserter"`
	TracingVersion    string  `env:"TRACING_VERSION"`
}

func NewConfig() (*Config, error) {
	var cfg Config

	err := env.Parse(&cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	return &cfg, nil
}
