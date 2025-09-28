package main

import (
	"context"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/hub"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
	"github.com/kstaniek/go-ampio-server/internal/serial"
	"github.com/kstaniek/go-ampio-server/internal/socketcan"
)

// fakeSerialPort implements serial.Port for tests.
type fakeSerialPort struct {
	reads [][]byte
	idx   int
	mu    sync.Mutex
}

func (f *fakeSerialPort) Read(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.idx >= len(f.reads) {
		// after delivering all data, block briefly then return EOF repeatedly
		time.Sleep(10 * time.Millisecond)
		return 0, io.EOF
	}
	chunk := f.reads[f.idx]
	f.idx++
	n := copy(p, chunk)
	return n, nil
}
func (f *fakeSerialPort) Write(p []byte) (int, error) { return len(p), nil }
func (f *fakeSerialPort) Close() error                { return nil }

// fakeSocketCAN implements minimal device surface we need (ReadFrame, Close, WriteFrame).
// We duplicate a tiny subset here to avoid importing linux syscalls in test fakes.

// TestInitSerialBackendBasic validates that a frame presented via the serial RX loop is decoded
// and broadcast to hub clients, and that serial RX metric increments.
func TestInitSerialBackendBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// prepare one CAN frame
	frame := can.Frame{CANID: (0x123 & can.CAN_EFF_MASK) | can.CAN_EFF_FLAG, Len: 2}
	frame.Data[0] = 0xAA
	frame.Data[1] = 0xBB
	// build RX wire frame: big-endian ID + payload enveloped
	rawID := frame.CANID & can.CAN_EFF_MASK
	data := make([]byte, 4+frame.Len)
	data[0] = byte(rawID >> 24)
	data[1] = byte(rawID >> 16)
	data[2] = byte(rawID >> 8)
	data[3] = byte(rawID)
	copy(data[4:], frame.Data[:frame.Len])
	enc := serTestWireEnvelope(data)

	openSerialPort = func(name string, baud int, to time.Duration) (serial.Port, error) {
		return &fakeSerialPort{reads: [][]byte{enc}}, nil
	}
	// restore after test
	defer func() { openSerialPort = serial.Open }()

	h := hub.New()
	c := &hub.Client{Out: make(chan can.Frame, 1), Closed: make(chan struct{})}
	h.Add(c)

	cfg := &appConfig{backend: "serial", serialDev: "fake", baud: 115200, serialReadTO: 50 * time.Millisecond}
	var wg sync.WaitGroup
	send, cleanup, err := initSerialBackend(ctx, cfg, h, testLogger(), &wg)
	if err != nil {
		t.Fatalf("initSerialBackend: %v", err)
	}
	defer cleanup()

	// wait for RX loop to process
	select {
	case fr := <-c.Out:
		if fr.CANID != frame.CANID || fr.Len != frame.Len || fr.Data[0] != frame.Data[0] {
			t.Fatalf("unexpected frame: %+v", fr)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for frame")
	}

	// send path sanity (should not error)
	if err := send(frame); err != nil {
		t.Fatalf("send frame: %v", err)
	}

	snap := metrics.Snap()
	if snap.SerialRx == 0 {
		t.Fatalf("expected SerialRx > 0, got %d", snap.SerialRx)
	}
}

// TestInitSocketCANBackendBasic ensures a frame is broadcast and metrics increment.
// testLogger returns a no-op slog.Logger for tests.
func testLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// serTestWireEnvelope replicates serial.canUARTSend (not exported) for tests.
func serTestWireEnvelope(data []byte) []byte {
	n := len(data)
	frame := make([]byte, n+4)
	frame[0] = 0x2D
	frame[1] = 0xD4
	frame[2] = byte(n + 1)
	sum := frame[2] + 0x2D
	for i, b := range data {
		frame[3+i] = b
		sum += b
	}
	frame[3+n] = sum
	return frame
}

// ---- SocketCAN backend test ----

type fakeSocketDev struct {
	frames   []can.Frame
	idx      int
	errAfter bool
}

func (d *fakeSocketDev) ReadFrame(fr *can.Frame) error {
	if d.idx < len(d.frames) {
		*fr = d.frames[d.idx]
		d.idx++
		return nil
	}
	if d.errAfter {
		return io.ErrUnexpectedEOF
	}
	time.Sleep(10 * time.Millisecond)
	return io.EOF
}
func (d *fakeSocketDev) WriteFrame(fr can.Frame) error { return nil }
func (d *fakeSocketDev) Close() error                  { return nil }

func TestInitSocketCANBackendBasic(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frame := can.Frame{CANID: 0x555, Len: 3}
	frame.Data[0], frame.Data[1], frame.Data[2] = 0x01, 0x02, 0x03

	openSocketCANDevice = func(iface string) (socketcan.Dev, error) {
		return &fakeSocketDev{frames: []can.Frame{frame}, errAfter: true}, nil
	}
	defer func() {
		openSocketCANDevice = func(iface string) (socketcan.Dev, error) { return socketcan.Open(iface) }
	}()

	h := hub.New()
	c := &hub.Client{Out: make(chan can.Frame, 1), Closed: make(chan struct{})}
	h.Add(c)
	cfg := &appConfig{backend: "socketcan", canIf: "vcan0"}
	var wg sync.WaitGroup
	send, cleanup, err := initSocketCANBackend(ctx, cfg, h, testLogger(), &wg)
	if err != nil {
		t.Fatalf("initSocketCANBackend: %v", err)
	}
	defer cleanup()

	select {
	case fr := <-c.Out:
		if fr.CANID != frame.CANID || fr.Len != frame.Len {
			t.Fatalf("unexpected frame: %+v", fr)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("timeout waiting for socketcan frame")
	}

	if err := send(frame); err != nil {
		t.Fatalf("send frame: %v", err)
	}
	// Allow read error path to trigger once.
	time.Sleep(30 * time.Millisecond)
	snap := metrics.Snap()
	if snap.SocketCANRx == 0 {
		t.Fatalf("expected SocketCANRx > 0")
	}
	if snap.Errors == 0 {
		t.Fatalf("expected at least one error increment (read error after frame)")
	}
}
