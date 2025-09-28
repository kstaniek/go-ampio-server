[![CI](https://github.com/kstaniek/go-ampio-server/actions/workflows/ci.yml/badge.svg)](https://github.com/kstaniek/go-ampio-server/actions/workflows/ci.yml)

## CAN ↔ TCP Cannelloni Gateway

Bridges a CAN backend (USB serial adapter or SocketCAN) to TCP clients speaking the cannelloni frame format:

```
	4 bytes  CAN ID (big endian, includes flags)
	1 byte   length (0..8 classic CAN; upper bit currently unused)
	0..8     payload bytes
	(repeated for each frame in a batch)
```

### Key Features
* Serial and SocketCAN backends (`--backend=serial|socketcan`)
* Broadcast hub with backpressure policies (drop or kick slow clients)
* Efficient batching writer (coalesces frames every 5ms or at batch size threshold)
* Prometheus metrics (always enabled) with lightweight in-process counters for logging
* Comprehensive tests: unit, integration smoke, stress, fuzz, benchmarks

---
### Installation
From source (requires Go ≥ 1.24):
```bash
go build ./cmd/can-server
```
Or install latest tag:
```bash
go install github.com/kstaniek/go-ampio-server/cmd/can-server@latest
```
Release artifacts (multi-arch Linux binaries, tar.gz archives, Debian packages, checksum manifest) are published for tagged versions (see GitHub Releases). A detached GPG signature for the checksum file may be added manually. No Homebrew formula/cask or container images are produced at this time.

Show version metadata:
```bash
./can-server -version
```

### Quick Start (SocketCAN)
```bash
sudo ./can-server -backend socketcan -can-if can0 -listen :20000
```

Serial backend:
```bash
sudo ./can-server -backend serial -serial /dev/ttyUSB0 -baud 115200 -listen :20000
```

### Flag Overview (subset)
```
	-backend serial|socketcan   CAN backend (default socketcan)
	-can-if can0                SocketCAN interface when backend=socketcan
	-serial /dev/ttyUSB0        Serial device path
	-baud 115200                Serial baud
	-listen :20000              TCP listen address
	-serial-read-timeout 50ms   Serial backend read timeout
	-hub-buffer 512             Per-client outbound frame buffer (channel size)
	-hub-policy drop|kick       Backpressure policy (see below)
	-max-clients 0              Max simultaneous TCP clients (0 = unlimited)
	-handshake-timeout 3s       Handshake (protocol hello) timeout
	-client-read-timeout 60s    Per-connection read deadline
	-mdns-enable true|false     Enable mDNS/Avahi advertisement (CLI default false; systemd unit enables by default)
	-metrics-addr :9100         Expose Prometheus metrics endpoint
	-log-metrics-interval 30s   Periodic log of counters (no Prometheus needed)
	-log-format text|json       Structured log output format
	-log-level debug|info|warn|error  Log verbosity (default info)
	-version                    Print version and exit
```

### Environment Variable Overrides
All flags (except `-version`) may be set via environment variables when the flag is not explicitly provided. Precedence: command‑line flag > environment variable > built‑in default.

| Flag | Environment Variable | Notes |
|------|----------------------|-------|
| -serial | CAN_SERVER_SERIAL | Serial device path |
| -baud | CAN_SERVER_BAUD | Integer >0 |
| -listen | CAN_SERVER_LISTEN | TCP listen addr |
| -serial-read-timeout | CAN_SERVER_SERIAL_READ_TIMEOUT | Go duration (e.g. 50ms, 2s) |
| -log-format | CAN_SERVER_LOG_FORMAT | text|json |
| -log-level | CAN_SERVER_LOG_LEVEL | debug|info|warn|error |
| -metrics-addr | CAN_SERVER_METRICS | Empty disables |
| -hub-buffer | CAN_SERVER_HUB_BUFFER | Integer >0 |
| -hub-policy | CAN_SERVER_HUB_POLICY | drop|kick |
| -backend | CAN_SERVER_BACKEND | serial|socketcan |
| -can-if | CAN_SERVER_IF | SocketCAN interface name |
| -max-clients | CAN_SERVER_MAX_CLIENTS | Integer >=0 |
| -handshake-timeout | CAN_SERVER_HANDSHAKE_TIMEOUT | Go duration >0 |
| -client-read-timeout | CAN_SERVER_CLIENT_READ_TIMEOUT | Go duration >0 |
| -log-metrics-interval | CAN_SERVER_LOG_METRICS_INTERVAL | Go duration (0 disables) |
| -mdns-enable | CAN_SERVER_MDNS_ENABLE | true/false / 1/0 / yes/no / on/off |
| -mdns-name | CAN_SERVER_MDNS_NAME | Instance name; empty -> auto (can-server-<hostname>) |

Examples:
```bash
CAN_SERVER_BACKEND=serial CAN_SERVER_SERIAL=/dev/ttyACM0 \
CAN_SERVER_MDNS_ENABLE=true CAN_SERVER_BAUD=230400 \
./can-server -listen :21000   # flag -listen overrides any env
```

Systemd unit (packaged) enables mDNS by default by passing `-mdns-enable true`. Disable via `/etc/default/can-server`:
```bash
CAN_SERVER_MDNS_ENABLE=false
```

### Backpressure Policies
| Policy | Behavior | Use Case |
|--------|----------|----------|
| drop   | Slow client silently loses excess frames; connection stays open | Passive monitoring tools where gaps are acceptable |
| kick   | Slow client channel overflow triggers connection close | Ensure misbehaving/slow consumers are removed |

### Virtual CAN (vcan) Setup (Linux)
```bash
sudo modprobe vcan
sudo ip link add name vcan0 type vcan
sudo ip link set vcan0 up
```
Test with `candump vcan0` (from can-utils) while connecting TCP clients.

### Raspberry Pi / Dual-Channel CAN HAT
For detailed Raspberry Pi setup with the Waveshare 2‑CH CAN HAT (MCP2515) at Ampio 50 kbps (SPI overlays, persistent bitrate, txqueuelen, systemd integration) see: [2-CH-CAN-HAT.md](./2-CH-CAN-HAT.md).

### Metrics
Prometheus metrics (HTTP endpoint enabled only when you pass `-metrics-addr`):
```bash
./can-server -metrics-addr :9100 &
curl -s localhost:9100/metrics | grep tcp_tx_frames_total
```
Counter names:
```
	serial_rx_frames_total   Frames decoded from serial (or SocketCAN ingress mirror)
	serial_tx_frames_total   Frames transmitted to serial / SocketCAN
	tcp_rx_frames_total      Frames received from TCP clients
	tcp_tx_frames_total      Frames sent to TCP clients (after batching)
	hub_dropped_frames_total Frames dropped due to backpressure
	hub_kicked_clients_total Clients disconnected due to backpressure (kick policy)
	hub_rejected_clients_total Clients rejected (e.g., max-clients limit)
	hub_broadcast_fanout     Number of clients targeted in last broadcast
	hub_active_clients       Currently active clients
	hub_queue_depth_max      Max queued frames among clients in last sample
	hub_queue_depth_avg      Avg queued frames per client in last sample
	errors_total{where="…"}  Error counters by subsystem
	malformed_frames_total   Malformed protocol frames rejected
	build_info{version,commit,date} Value always 1 with build metadata labels
```
Counters are always incremented in-process; if you do not enable the HTTP endpoint you can still obtain a snapshot via internal calls to `metrics.Snap()` (used in tests / optional periodic logging).

### Architecture & Extensibility
`server.Server` depends only on small interfaces (see `internal/transport`):
* FrameDecoder / MultiFrameDecoder (batch decode)
* Encode / EncodeTo batching encoder
* SendFunc abstraction for backend TX

Replacing the wire codec (e.g. for filtering or logging) only requires implementing those interfaces.


### Testing & Quality
Basic tests:
```bash
go test ./...
```
Race + coverage:
```bash
make test-race
make cover   # outputs coverage.out & coverage.html
```
Integration smoke / stress (see `internal/server/smoke_test.go`):
```bash
go test -run TestSmoke ./internal/server       # core smoke set
go test -run TestStressBroadcast ./internal/server
```
Make targets:
```bash
make smoke-test   # minimal smoke
make stress       # stress broadcast
```

Benchmarks:
```bash
go test -run=^$ -bench=. -benchmem ./internal/cnl
```

Fuzz (short examples):
```bash
go test -run=^$ -fuzz=FuzzCodecRoundTrip -fuzztime=10s ./internal/cnl
go test -run=FuzzCodecDecode -fuzz=FuzzCodecDecode -fuzztime=10s ./internal/server
```
CI runs: format + vet + staticcheck + govulncheck, tests (race), smoke, fuzz (short), stress, builds & release packaging (on tags).

### Packaging
`make dist` produces tarballs for linux/amd64 and linux/arm64 (manual path – CI uses GoReleaser).
Debian package (if packaging templates present):
```bash
make deb DEBARCH=amd64   # or arm64
```

### Operational Notes
* Batching writer flushes every 5ms or when batch size (64 frames) is reached.
* Kick policy proactively closes slow consumers to prevent unbounded latency for others.
* Use Prometheus or periodic logging to spot hub drops (tune `-hub-buffer`).
* For production consider running under systemd with Restart=on-failure.

## Verifying Releases
### Signatures

If a GPG detached signature accompanies `sha256sums.txt`, verify it and an artifact:

```
gpg --verify sha256sums.txt.asc sha256sums.txt
grep <archive> sha256sums.txt | sha256sum -c -
```

Cosign provenance/signatures and container images are intentionally out of scope for now.
### Cannelloni Compatibility
Implements cannelloni-style DATA frame packing (no ACK/NACK control frames). Each frame is independent; ordering is preserved within a TCP stream.

### Security Considerations
* No authentication – place behind a firewall or run on trusted networks.
* Malformed frames are validated (length >8 rejected) and close offending connections.
* Fuzz tests run in CI to reduce parser crash risk.

### Contributing
Pull requests welcome. Please run:
```bash
make fmt-check test-race lint
```
before submitting. Add or update tests for new protocol behavior.

### License
MIT (see `LICENSE`).

## Systemd service

A sample unit is provided under `packaging/deb/etc/systemd/system/can-server.service` with defaults in `packaging/deb/etc/default/can-server`.

Quick install (manual copy):

```bash
sudo install -m755 bin/can-server /usr/bin/can-server
sudo install -m644 packaging/deb/etc/systemd/system/can-server.service /etc/systemd/system/can-server.service
sudo install -m644 packaging/deb/etc/default/can-server /etc/default/can-server
sudo systemctl daemon-reload
sudo systemctl enable --now can-server
```

Adjust `/etc/default/can-server` to set backend, interface/device, logging, metrics, and client limits/timeouts.

Health and metrics:
- Readiness endpoint: `curl -s localhost:9100/ready` (requires `-metrics-addr`) returns `ready` when backend + TCP listener are up.
- Prometheus metrics at `/metrics` when `-metrics-addr` is set.

Troubleshooting:
- `journalctl -u can-server -f` to stream logs (structured `slog`).
- Increase verbosity via `CAN_SERVER_LOG_LEVEL=debug` in `/etc/default/can-server`.
- For SocketCAN, ensure `can0` exists (`ip link add can0 type vcan; ip link set can0 up` for testing).
- For serial, check device permissions or run as root.

## Release Process

Strict tag format: x.y.z (no leading v). CI rejects anything else.

Minimal release flow:
1. Ensure tests pass:
	```bash
	make test-race smoke-test
	```
2. Choose next version (x.y.z) and create annotated tag:
	```bash
	git tag -a 0.2.0 -m "Release 0.2.0"
	```
3. (Optional) Dry run packaging locally:
	```bash
	make goreleaser-snapshot
	```
4. Push branch & tag:
	```bash
	git push origin main 0.2.0
	```
5. Wait for CI release job, then verify artifacts/signatures.

Notes:
* Release notes are auto-generated from commit history via GoReleaser's GitHub changelog; no local CHANGELOG.md is maintained.
* Prefix commits you want excluded with docs:, chore:, or test: (matched by GoReleaser filters).

If you need to redo a tag (forgot to update something):
```bash
make retag TAG=0.2.0
```

Fixing a bad tag:
- Delete local/remote: git tag -d 0.2.0 && git push origin :refs/tags/0.2.0
- Recreate correct tag and push again.

Tip: Install pre-push hook to enforce tag format:
  make install-hooks

Hooks installed:
	pre-commit  - go fmt check, vet staged files, block bin/ & dist/
	pre-push    - enforce tag format x.y.z (no leading v)

### Goreleaser

`.goreleaser.yaml` provides:
* Single build (metrics always compiled; no build tags required)
* Multi-arch (linux amd64/arm64) binaries
* Tar.gz archives + checksum manifest
* Debian package (systemd unit & default config installed)
* (Optional, manual) GPG signature for checksum manifest

Local dry run (snapshot, no publish):
```bash
go install github.com/goreleaser/goreleaser/v2/cmd/goreleaser@latest
make goreleaser-snapshot
```
Full release (tag on HEAD):
```bash
make goreleaser-release
```

Tag must be `x.y.z` (no leading v) to match CI/tag checks.

### Future

Potential future enhancements (not planned short-term): optional container images, provenance attestations.