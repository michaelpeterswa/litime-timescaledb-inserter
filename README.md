# litime-timescaledb-inserter

Polls one or more LiTime LiFePO4 batteries over Bluetooth LE and writes their
readings into TimescaleDB.

## Configuration

| Variable | Default | Description |
| --- | --- | --- |
| `LITIME_BATTERIES` | | Batteries to poll, as `id=target` pairs separated by commas. |
| `LITIME_BATTERY_BLUETOOTH_NAME` | | Single battery by advertised name. Used only when `LITIME_BATTERIES` is unset. |
| `BLUETOOTH_ADAPTER` | | Adapter to use, e.g. `hci1`. Empty uses the default (`hci0`). Linux only. |
| `TIMESCALE_CONN_STRING` | *required* | PostgreSQL/TimescaleDB connection string. |
| `SCRAPE_INTERVAL` | `10s` | How often each battery is asked for a reading. |
| `SCAN_TIMEOUT` | `30s` | How long to scan when resolving batteries by name. |
| `STALE_TIMEOUT` | `90s` | How long a battery may stay silent before it is reconnected. |
| `CALLBACK_TIMEOUT` | `5s` | Timeout for a single database insert. |
| `READING_BUFFER_SIZE` | `256` | Queue depth between Bluetooth and the database writer. |
| `LOG_LEVEL` | `error` | `debug`, `info`, `warn` or `error`. |
| `METRICS_ENABLED` / `METRICS_PORT` | `true` / `8081` | Prometheus metrics endpoint. |
| `TRACING_ENABLED` / `TRACING_SAMPLERATE` | `false` / `0.01` | OpenTelemetry tracing. |

### Configuring batteries

Each entry maps a battery ID to either a Bluetooth device address or an
advertised local name:

```sh
LITIME_BATTERIES="garage=11:22:33:AA:BB:CC,shed=11:22:33:AA:BB:CD"
```

The key/value separator is `=`, not `:`, because MAC addresses are full of
colons. The ID on the left is yours to choose and is stored with every reading,
so keep it stable: it is the only thing tying a row to a physical battery.

A value is treated as an address if it parses as one, and as an advertised name
otherwise. Which interpretation was chosen is logged at startup:

```json
{"msg":"battery configured","battery_id":"garage","match":"address","value":"11:22:33:AA:BB:CC"}
```

Check that line if a battery is never found — a mistyped address shows up here as
`"match":"name"`.

### Prefer addresses over names

Names are only usable when every battery advertises a distinct one. Batteries of
the same model often share a name, and there is no way to tell them apart, so
readings would be attributed to whichever the adapter happened to surface first.
Startup fails with the addresses to use if more than one device answers to a
configured name.

To find your addresses, run the discovery example from the
[go-litime-bluetooth](https://github.com/alpineworks/go-litime-bluetooth)
repository on the host that has the radio:

```sh
go run ./example/multi
```

Addresses are MAC addresses on Linux but opaque CoreBluetooth UUIDs on macOS, so
a value discovered on one operating system will not work on another.

### WiFi and Bluetooth on a Raspberry Pi

The Pi's onboard chip shares a single antenna between WiFi and Bluetooth. A busy
2.4GHz WiFi link, especially a weak one where the radio transmits at high power
for long stretches, starves Bluetooth badly enough to break it. The signature is
distinctive and easy to misread as faulty hardware:

- Scanning works and devices appear with a strong RSSI
- Connections are *established* and then dropped within about half a second
- `bluetoothctl` reports `le-connection-abort-by-local`, and `btmon` shows
  `LE Connection Complete` followed by `Reason: Connection Failed to be
  Established (0x3e)`
- It affects every device, not just batteries, and survives reboots

To confirm it, block WiFi briefly and retry the connection:

```sh
sudo rfkill block wifi && sleep 5
bluetoothctl connect <address>
sudo rfkill unblock wifi
```

If that connects, this is the problem. Fixes, best first: use Ethernet and
disable WiFi; move Bluetooth to a USB dongle and select it with
`BLUETOOTH_ADAPTER=hci1` (disable the onboard radio with `dtoverlay=disable-bt`
so numbering stays stable); or improve the WiFi signal to reduce airtime.

## How it works

All batteries share a single Bluetooth radio, which constrains the design:

- **Every connection attempt scans first.** BlueZ can only connect to a device it
  currently holds an object for, and it discards those over time, so a scan is
  what makes a device connectable — including on reconnect and including when
  the address is already known. Configuring an address narrows the scan rather
  than skipping it.
- **Scanning and connecting are serialised across batteries.** An adapter runs
  one scan at a time, and a controller establishes one connection at a time.
  Overlapping attempts abort each other, which shows up as a battery that was
  working failing when another is added.
- **Supervision is per battery.** Each is retried on its own goroutine with
  exponential backoff, so one flat or out-of-range battery never stalls the
  others.
- **Silence is the liveness signal.** As a Bluetooth central there is no
  notification when a peer disappears, so a battery that has not produced a
  reading within `STALE_TIMEOUT` is disconnected and reconnected. Keep
  `STALE_TIMEOUT` comfortably above `SCRAPE_INTERVAL` so a single missed reply
  does not force a reconnect.
- **Database writes are off the Bluetooth path.** Readings go through a bounded
  queue to a separate writer, so a slow database cannot stall notification
  dispatch. If the queue fills, readings are dropped and counted rather than
  queued without limit.

Note that a controller supports a limited number of simultaneous LE connections,
typically single digits, which caps how many batteries one host can poll.

## Metrics

Per-battery series, all labelled `battery_id`, since with several batteries the
question is usually whether one of them has quietly died:

| Metric | Description |
| --- | --- |
| `litime_battery_connected` | `1` while connected, `0` otherwise. |
| `litime_battery_notifications` | Readings received. |
| `litime_battery_reconnects` | Connections re-established. |
| `litime_battery_readings_dropped` | Readings discarded because the queue was full. |
| `litime_battery_parse_failures` | Notifications that could not be parsed. |
| `litime_inserts` / `litime_insert_errors` | Database write outcomes. |

## Schema

Readings land in `sensors.litime`, a hypertable keyed on `time` with a
`battery_id` column identifying the source. Rows written before multi-battery
support are backfilled as `unknown`.

## Development

```sh
docker compose up --build
```

Brings up the inserter alongside TimescaleDB, Grafana, Prometheus, Tempo and
pgAdmin. The container needs `/var/run/dbus` and `privileged: true` to reach the
host's Bluetooth adapter.
