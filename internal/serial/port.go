package serial

import (
	"time"

	"github.com/tarm/serial"
)

// Port abstracts tarm/serial for testability.
type Port interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Close() error
}

func Open(name string, baud int, readTimeout time.Duration) (Port, error) {
	cfg := &serial.Config{Name: name, Baud: baud, ReadTimeout: readTimeout}
	return serial.OpenPort(cfg)
}
