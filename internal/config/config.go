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
