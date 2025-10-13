module github.com/kstaniek/go-ampio-server

go 1.24.0

toolchain go1.24.6

require (
	github.com/grandcat/zeroconf v1.0.0
	github.com/prometheus/client_golang v1.23.2
	github.com/tarm/serial v0.0.0-20180830185346-98f6abe2eb07
	golang.org/x/sys v0.37.0
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cenkalti/backoff v2.2.1+incompatible // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/kr/text v0.2.0 // indirect
	github.com/miekg/dns v1.1.27 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	go.yaml.in/yaml/v2 v2.4.2 // indirect
	golang.org/x/crypto v0.41.0 // indirect
	golang.org/x/net v0.43.0 // indirect
	google.golang.org/protobuf v1.36.8 // indirect
)

// Runtime dependencies only. 'go mod tidy' will add minimal indirects.
