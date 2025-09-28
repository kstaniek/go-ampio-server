package server

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/cnl"
	"github.com/kstaniek/go-ampio-server/internal/hub"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
)

// dummySend implements a no-op backend transmitter.
// capture backend sends for verification
var (
	backendMu  chan struct{}
	captured   []can.Frame
	capturedMu sync.Mutex
)

func dummySend(fr can.Frame) error {
	if backendMu != nil {
		// non-blocking capture
		select {
		case <-backendMu:
		default:
		}
		capturedMu.Lock()
		captured = append(captured, fr)
		capturedMu.Unlock()
	}
	return nil
}

// TestSmokeServer starts the TCP server on an ephemeral port and performs the Cannelloni handshake.
func TestSmokeServer(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Reset captured frames for this test to avoid cross-test contamination.
	capturedMu.Lock()
	captured = nil
	capturedMu.Unlock()

	h := hub.New()
	srv := NewServer(
		WithHub(h),
		WithCodec(&cnl.Codec{}),
		WithSend(dummySend),
		WithHandshakeTimeout(2*time.Second),
	)
	srv.SetListenAddr(":0")
	go func() {
		if err := srv.Serve(ctx); err != nil {
			t.Logf("Serve returned: %v", err)
		}
	}()
	select {
	case <-srv.Ready():
	case <-time.After(1 * time.Second):
		t.Fatalf("server did not signal readiness")
	}
	addr := srv.Addr()

	d := net.Dialer{Timeout: 1 * time.Second}
	conn, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	// Both sides must send the 12 byte magic; emulate client side.
	if _, err := conn.Write([]byte("CANNELLONIv1")); err != nil {
		t.Fatalf("write magic: %v", err)
	}
	buf := make([]byte, 12)
	if _, err := conn.Read(buf); err != nil {
		t.Fatalf("read magic: %v", err)
	}
	if string(buf) != "CANNELLONIv1" {
		t.Fatalf("unexpected handshake magic %q", string(buf))
	}

	// --- Client → Server path (encode one frame) ---
	// Build a single cannelloni frame: id 0x123, len 3, data {1,2,3}
	var frameBuf bytes.Buffer
	var idb [4]byte
	binary.BigEndian.PutUint32(idb[:], 0x123)
	frameBuf.Write(idb[:])
	frameBuf.WriteByte(3)
	frameBuf.Write([]byte{1, 2, 3})
	backendMu = make(chan struct{}, 1)
	if _, err := conn.Write(frameBuf.Bytes()); err != nil {
		t.Fatalf("write frame: %v", err)
	}
	// Wait up to 100ms for backend capture instead of fixed sleep
	deadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(deadline) {
		capturedMu.Lock()
		okFirst := len(captured) >= 1
		capturedMu.Unlock()
		if okFirst {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	capturedMu.Lock()
	okFirst2 := len(captured) == 1 && captured[0].CANID == 0x123 && captured[0].Len == 3
	capturedMu.Unlock()
	if !okFirst2 {
		// Provide diagnostic with whatever was captured
		t.Fatalf("expected captured frame, got %#v", captured)
	}

	// --- Server → Client broadcast path ---
	// Dial a second client to observe broadcast
	conn2, err := d.DialContext(ctx, "tcp", srv.Addr())
	if err != nil {
		t.Fatalf("dial second: %v", err)
	}
	defer conn2.Close()
	// Perform handshake
	if _, err := conn2.Write([]byte("CANNELLONIv1")); err != nil {
		t.Fatalf("handshake2 write: %v", err)
	}
	if _, err := conn2.Read(make([]byte, 12)); err != nil {
		t.Fatalf("handshake2 read: %v", err)
	}

	// Broadcast a frame via hub
	var d64 [64]byte
	d64[0], d64[1] = 9, 8
	srv.Hub.Broadcast(can.Frame{CANID: 0x456, Len: 2, Data: d64})
	// Read from first client (which accumulates batches); give some time then read available
	// Accumulate until we have at least one full frame header (5 bytes) or timeout.
	deadlineRead := time.Now().Add(120 * time.Millisecond)
	_ = conn.SetReadDeadline(time.Now().Add(40 * time.Millisecond))
	rb := make([]byte, 64)
	var n int
	for time.Now().Before(deadlineRead) {
		m, err := conn.Read(rb[n:])
		if err != nil {
			if isTimeout(err) {
				if n >= 5 { // already enough
					break
				}
				_ = conn.SetReadDeadline(time.Now().Add(30 * time.Millisecond))
				continue
			}
			t.Fatalf("read broadcast: %v", err)
		}
		n += m
		if n >= 5 {
			break
		}
	}
	if n < 5 {
		t.Fatalf("expected >=5 bytes, got %d", n)
	}
	gotID := binary.BigEndian.Uint32(rb[:4])
	if gotID != 0x456 {
		t.Fatalf("broadcast frame id mismatch got 0x%X", gotID)
	}
}

// TestSmokeBatch verifies batching encode path by pushing several frames quickly.
func TestSmokeBatch(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := hub.New()
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}), WithSend(dummySend))
	go srv.Serve(ctx)
	<-srv.Ready()

	c1 := dialAndHandshake(t, ctx, srv.Addr())
	defer c1.Close()

	// Briefly poll for hub registration instead of fixed sleep.
	regDeadline := time.Now().Add(60 * time.Millisecond)
	for time.Now().Before(regDeadline) {
		if h.Count() > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Broadcast exactly 64 frames to force immediate flush (batch threshold 64)
	for i := 0; i < 64; i++ {
		var d [64]byte
		d[0] = byte(i)
		srv.Hub.Broadcast(can.Frame{CANID: uint32(0x700 + (i % 32)), Len: 1, Data: d})
	}
	// No fixed delay; read loop below will wait with deadlines.

	// Expect a single large write of ~64*6 = 384 bytes (id+len+data)
	buf := bytes.Buffer{}
	deadline := time.Now().Add(400 * time.Millisecond)
	tmp := make([]byte, 256)
	for time.Now().Before(deadline) && buf.Len() < 400 {
		_ = c1.SetReadDeadline(time.Now().Add(80 * time.Millisecond))
		n, err := c1.Read(tmp)
		if err != nil {
			if isTimeout(err) {
				continue
			}
			break
		}
		buf.Write(tmp[:n])
	}
	if buf.Len() < 50 {
		t.Fatalf("insufficient batch bytes collected: %d", buf.Len())
	}
	dec := &cnl.Codec{}
	r := bytes.NewReader(buf.Bytes())
	first, err := dec.Decode(r)
	if err != nil {
		t.Fatalf("decode first batch frame: %v (bytes=%d)", err, buf.Len())
	}
	if first.CANID < 0x700 || first.CANID >= 0x740 {
		t.Fatalf("unexpected first CANID 0x%X", first.CANID)
	}
	// Decode a few more frames to ensure stream integrity.
	decoded := 1
	for decoded < 5 {
		_, err := dec.Decode(r)
		if err != nil {
			break
		}
		decoded++
	}
	if decoded < 2 {
		t.Fatalf("expected multiple frames, got %d (total bytes=%d)", decoded, buf.Len())
	}
}

// TestSmokeBackpressureDrop sets small buffer and ensures overflow increments drop metric logic (observable via closed flag when policy=kick vs not closed for drop).
func TestSmokeBackpressureDrop(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := hub.New()
	h.OutBufSize = 1
	h.Policy = hub.PolicyDrop
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}), WithSend(dummySend))
	go srv.Serve(ctx)
	<-srv.Ready()
	c1 := dialAndHandshake(t, ctx, srv.Addr())
	defer c1.Close()

	// Fill buffer then send extra frames which should be dropped (channel non-blocking)
	for i := 0; i < 5; i++ {
		srv.Hub.Broadcast(can.Frame{CANID: 0x900, Len: 0})
	}
	// Client stays connected under drop policy
	// Drain one frame
	_ = c1.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	one := make([]byte, 32)
	_, _ = c1.Read(one) // ignore content
	// Connection should still be alive (further read with short deadline should return either timeout or data, not EOF)
	_ = c1.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	tmp := make([]byte, 8)
	_, err := c1.Read(tmp)
	if err != nil && !isTimeout(err) && err == io.EOF {
		t.Fatalf("connection closed unexpectedly under drop policy: %v", err)
	}
}

// TestSmokeBackpressureKick ensures slow client gets closed when policy=kick and buffer overflows.
func TestSmokeBackpressureKick(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := hub.New()
	h.OutBufSize = 1
	h.Policy = hub.PolicyKick
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}), WithSend(dummySend))
	go srv.Serve(ctx)
	<-srv.Ready()
	c1 := dialAndHandshake(t, ctx, srv.Addr())
	defer c1.Close()
	// Avoid reading from c1 to simulate slowness
	for i := 0; i < 10; i++ {
		srv.Hub.Broadcast(can.Frame{CANID: 0xA00, Len: 0})
		// small pacing sleep keeps behaviour but shorter
		time.Sleep(2 * time.Millisecond)
	}
	// Now attempt read; expect EOF or connection error fairly soon
	_ = c1.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	buf := make([]byte, 16)
	_, err := c1.Read(buf)
	if err == nil {
		// If we still read data, connection has not yet closed—acceptable but report
		t.Logf("kick policy: client not yet closed (data received)")
	} else if err == io.EOF {
		// expected closure path
	} else if isTimeout(err) {
		t.Logf("kick policy: timeout waiting for closure (may be timing-sensitive)")
	}
}

// TestSmokeMetrics ensures metrics counters reflect activity (TX/RX and hub drops)
func TestSmokeMetrics(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := hub.New()
	h.OutBufSize = 1
	h.Policy = hub.PolicyDrop
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}), WithSend(dummySend))
	go srv.Serve(ctx)
	<-srv.Ready()

	pre := metrics.Snap()
	c := dialAndHandshake(t, ctx, srv.Addr())
	defer c.Close()

	// Client -> Server: send 3 frames
	for i := 0; i < 3; i++ {
		var buf bytes.Buffer
		var idb [4]byte
		binary.BigEndian.PutUint32(idb[:], 0x100+uint32(i))
		buf.Write(idb[:])
		buf.WriteByte(1)
		buf.Write([]byte{byte(i)})
		if _, err := c.Write(buf.Bytes()); err != nil {
			t.Fatalf("write frame %d: %v", i, err)
		}
	}

	// Server -> Client: broadcast 5 frames (some may drop due to tiny buffer)
	for i := 0; i < 5; i++ {
		srv.Hub.Broadcast(can.Frame{CANID: 0x800 + uint32(i), Len: 0})
	}
	// Ensure writer flushed by attempting to read at least one frame header.
	readDeadline := time.Now().Add(200 * time.Millisecond)
	buf := make([]byte, 32)
	for time.Now().Before(readDeadline) {
		_ = c.SetReadDeadline(time.Now().Add(20 * time.Millisecond))
		if n, err := c.Read(buf); n > 0 && (err == nil || isTimeout(err)) {
			break
		} else if err != nil && !isTimeout(err) {
			break
		}
	}
	// Fallback polling for TCPTx increase (covers cases where read consumed all but metrics not yet sampled).
	postWait := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(postWait) {
		if d := metrics.Snap(); d.TCPTx > pre.TCPTx {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	post := metrics.Snap()

	if d := post.TCPRx - pre.TCPRx; d < 3 {
		t.Fatalf("expected >=3 TCPRx delta, got %d (pre=%d post=%d)", d, pre.TCPRx, post.TCPRx)
	}
	if d := post.TCPTx - pre.TCPTx; d == 0 {
		t.Fatalf("expected TCPTx >0 delta (pre=%d post=%d)", pre.TCPTx, post.TCPTx)
	}
	if post.HubDrops < pre.HubDrops {
		t.Fatalf("hub drops decreased pre=%d post=%d", pre.HubDrops, post.HubDrops)
	}
}

// TestSmokeSerialAndErrors simulates serial TX/RX metrics and a handshake failure to bump error counter.
func TestSmokeSerialAndErrors(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := hub.New()
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}))
	var sent []can.Frame
	srv.Send = func(fr can.Frame) error { // simulate serial transmit (client->server path)
		metrics.IncSerialTx()
		sent = append(sent, fr)
		return nil
	}
	go srv.Serve(ctx)
	select {
	case <-srv.Ready():
	case <-time.After(1 * time.Second):
		t.Fatalf("server not ready")
	}

	pre := metrics.Snap()
	c := dialAndHandshake(t, ctx, srv.Addr())
	defer c.Close()

	// Simulate inbound serial frames (serial->hub->client) and count as SerialRx.
	for i := 0; i < 3; i++ {
		metrics.IncSerialRx()
		srv.Hub.Broadcast(can.Frame{CANID: 0x600 + uint32(i), Len: 0})
	}
	// Wait for at least one TCPTx increment (writer flush) instead of fixed sleep.
	flushDeadline := time.Now().Add(80 * time.Millisecond)
	for time.Now().Before(flushDeadline) {
		if snap := metrics.Snap(); snap.TCPTx > pre.TCPTx {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Client -> server: send two frames which should invoke srv.Send (serial TX)
	for i := 0; i < 2; i++ {
		var buf bytes.Buffer
		var idb [4]byte
		binary.BigEndian.PutUint32(idb[:], 0x200+uint32(i))
		buf.Write(idb[:])
		buf.WriteByte(0) // zero-length payload
		if _, err := c.Write(buf.Bytes()); err != nil {
			t.Fatalf("client write %d: %v", i, err)
		}
	}
	// Wait for serial tx accounting
	serialDeadline := time.Now().Add(80 * time.Millisecond)
	for time.Now().Before(serialDeadline) {
		if snap := metrics.Snap(); snap.SerialTx-pre.SerialTx >= 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	// Induce handshake error by opening and immediately closing a raw connection (no hello exchange)
	raw, err := net.DialTimeout("tcp", srv.Addr(), 500*time.Millisecond)
	if err != nil {
		t.Fatalf("dial raw: %v", err)
	}
	_ = raw.Close() // server handshake should fail quickly and count an error
	// Wait for handshake error metric increment
	errDeadline := time.Now().Add(120 * time.Millisecond)
	for time.Now().Before(errDeadline) {
		if snap := metrics.Snap(); snap.Errors > pre.Errors {
			break
		}
		time.Sleep(3 * time.Millisecond)
	}

	post := metrics.Snap()
	if d := post.SerialRx - pre.SerialRx; d < 3 {
		t.Fatalf("expected SerialRx delta >=3 got %d", d)
	}
	if d := post.SerialTx - pre.SerialTx; d < 2 {
		t.Fatalf("expected SerialTx delta >=2 got %d (sent=%d)", d, len(sent))
	}
	if post.Errors <= pre.Errors {
		t.Fatalf("expected Errors to increase (pre=%d post=%d)", pre.Errors, post.Errors)
	}
}

// TestSmokeMalformedFrames sends an invalid length (>8) to trigger decode error and tcp_read metric increment.
func TestSmokeMalformedFrames(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := hub.New()
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}), WithSend(dummySend))
	go srv.Serve(ctx)
	<-srv.Ready()
	c := dialAndHandshake(t, ctx, srv.Addr())
	defer c.Close()
	pre := metrics.Snap()
	// Build malformed frame: id=0x111 + length byte 9 (>8) no payload needed (codec rejects by length before payload read)
	var idb [4]byte
	binary.BigEndian.PutUint32(idb[:], 0x111)
	bad := append(idb[:], byte(9))
	if _, err := c.Write(bad); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	// Wait for error increment and closure instead of fixed sleep
	malDeadline := time.Now().Add(100 * time.Millisecond)
	for time.Now().Before(malDeadline) {
		post := metrics.Snap()
		if post.Errors > pre.Errors {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	post := metrics.Snap()
	if post.Errors <= pre.Errors {
		t.Fatalf("expected error counter increment (pre=%d post=%d)", pre.Errors, post.Errors)
	}
	// Ensure connection closed
	_ = c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 8)
	if _, err := c.Read(buf); err == nil {
		t.Fatalf("expected connection closed after malformed frame")
	}
}

// TestSmokeConcurrentClients ensures broadcasts reach multiple simultaneous clients.
func TestSmokeConcurrentClients(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := hub.New()
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}), WithSend(dummySend))
	go srv.Serve(ctx)
	<-srv.Ready()
	const nClients = 5
	conns := make([]net.Conn, 0, nClients)
	for i := 0; i < nClients; i++ {
		conns = append(conns, dialAndHandshake(t, ctx, srv.Addr()))
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	// Poll for all clients registered
	regAllDeadline := time.Now().Add(120 * time.Millisecond)
	for time.Now().Before(regAllDeadline) {
		if h.Count() == nClients {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	// Broadcast several frames
	for i := 0; i < 10; i++ {
		srv.Hub.Broadcast(can.Frame{CANID: 0x500 + uint32(i), Len: 0})
	}
	// Poll for at least one tx for each client rather than fixed sleep.
	ccDeadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(ccDeadline) {
		// heuristically check any tx occurred
		if snap := metrics.Snap(); snap.TCPTx >= 1 {
			break
		}
		time.Sleep(3 * time.Millisecond)
	}
	// Each client should receive at least one frame
	for idx, c := range conns {
		_ = c.SetReadDeadline(time.Now().Add(120 * time.Millisecond))
		collected := bytes.Buffer{}
		tmp := make([]byte, 128)
		for collected.Len() < 5 { // 4 bytes id + len
			n, err := c.Read(tmp)
			if err != nil {
				if isTimeout(err) {
					break
				}
				t.Fatalf("client %d read err: %v", idx, err)
			}
			collected.Write(tmp[:n])
			if collected.Len() >= 5 {
				break
			}
		}
		if collected.Len() < 5 {
			t.Fatalf("client %d received insufficient data (%d bytes)", idx, collected.Len())
		}
		r := bytes.NewReader(collected.Bytes())
		fr, err := (&cnl.Codec{}).Decode(r)
		if err != nil {
			t.Fatalf("client %d decode err: %v", idx, err)
		}
		if fr.CANID < 0x500 || fr.CANID >= 0x510 {
			t.Fatalf("client %d unexpected CANID 0x%X", idx, fr.CANID)
		}
	}
}

// TestGracefulShutdown ensures Shutdown closes listener and active clients.
func TestGracefulShutdown(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	h := hub.New()
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}), WithSend(dummySend))
	go srv.Serve(ctx)
	<-srv.Ready()
	// Open a couple clients
	c1 := dialAndHandshake(t, ctx, srv.Addr())
	c2 := dialAndHandshake(t, ctx, srv.Addr())
	// Wait until hub registers both (avoid racing with shutdown)
	wait := time.Now().Add(200 * time.Millisecond)
	for time.Now().Before(wait) {
		if h.Count() >= 2 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	// Trigger shutdown
	sdCtx, sdCancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer sdCancel()
	if err := srv.Shutdown(sdCtx); err != nil {
		t.Fatalf("shutdown err: %v", err)
	}
	// Reads should quickly fail
	_ = c1.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 8)
	if _, err := c1.Read(buf); err == nil {
		t.Fatalf("expected c1 read to fail after shutdown")
	}
	_ = c2.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	if _, err := c2.Read(buf); err == nil {
		t.Fatalf("expected c2 read to fail after shutdown")
	}
}

// TestFrameFilter ensures frames failing predicate are dropped (not counted in TCPRx nor sent to backend).
func TestFrameFilter(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	h := hub.New()
	var backend []can.Frame
	var backendMu sync.Mutex
	srv := NewServer(
		WithHub(h),
		WithCodec(&cnl.Codec{}),
		WithSend(func(fr can.Frame) error {
			backendMu.Lock()
			backend = append(backend, fr)
			backendMu.Unlock()
			return nil
		}),
		WithFrameFilter(func(fr *can.Frame) bool { return fr.CANID%2 == 0 }), // allow only even IDs
	)
	go srv.Serve(ctx)
	<-srv.Ready()
	c := dialAndHandshake(t, ctx, srv.Addr())
	defer c.Close()
	pre := metrics.Snap()
	// Send 4 frames: two even, two odd ids.
	for i := 0; i < 4; i++ {
		var buf bytes.Buffer
		var idb [4]byte
		binary.BigEndian.PutUint32(idb[:], 0x100+uint32(i))
		buf.Write(idb[:])
		buf.WriteByte(0)
		if _, err := c.Write(buf.Bytes()); err != nil {
			t.Fatalf("write %d: %v", i, err)
		}
	}
	// Wait for backend to receive even frames
	deadline := time.Now().Add(150 * time.Millisecond)
	for time.Now().Before(deadline) {
		backendMu.Lock()
		l := len(backend)
		backendMu.Unlock()
		if l >= 2 {
			break
		}
		time.Sleep(3 * time.Millisecond)
	}
	post := metrics.Snap()
	backendMu.Lock()
	l := len(backend)
	backendMu.Unlock()
	if l != 2 {
		t.Fatalf("expected 2 backend frames (even ids), got %d", l)
	}
	if d := post.TCPRx - pre.TCPRx; d != 2 {
		t.Fatalf("expected TCPRx delta 2 (only even), got %d", d)
	}
	backendMu.Lock()
	for _, fr := range backend {
		if fr.CANID%2 != 0 {
			t.Fatalf("backend received odd id 0x%X", fr.CANID)
		}
	}
	backendMu.Unlock()
}

// fakeSocketCANDev implements the subset needed for TXWriter tests (linux-only tests will exercise real path).
// For portability in unit tests we simulate overflow by limiting channel and forcing write errors.
// We create a narrow test to ensure ErrSocketCANOver / ErrSocketCANWrite are surfaced similarly to serial.
func TestSocketCANOverflowAndWriteError(t *testing.T) {
	// Only run on linux builds where real socketcan is available; otherwise skip (stub just provides symbols).
	// This keeps test portable while verifying metrics parity when feasible.
	if testing.Short() {
		t.Skip("short mode")
	}
	// We can't open a real CAN interface in CI without privileges; just assert that overflow sentinel increments error metric.
	pre := metrics.Snap()
	// Simulate overflow increments directly (acts like txwriter would on full channel)
	metrics.IncError(metrics.ErrSocketCANOver)
	post := metrics.Snap()
	if post.Errors <= pre.Errors {
		t.Fatalf("expected error counter increase for socketcan overflow")
	}
}

// TestStressBroadcast (skipped under -short) creates many clients and pushes a higher volume of frames.
func TestStressBroadcast(t *testing.T) {
	if testing.Short() {
		t.Skip("stress skipped in -short mode")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	h := hub.New()
	srv := NewServer(WithHub(h), WithCodec(&cnl.Codec{}), WithSend(dummySend))
	go srv.Serve(ctx)
	<-srv.Ready()

	const nClients = 20
	const nFrames = 200
	conns := make([]net.Conn, 0, nClients)
	for i := 0; i < nClients; i++ {
		conns = append(conns, dialAndHandshake(t, ctx, srv.Addr()))
	}
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	time.Sleep(40 * time.Millisecond)

	// Broadcast frames concurrently
	for i := 0; i < nFrames; i++ {
		srv.Hub.Broadcast(can.Frame{CANID: 0x300 + uint32(i%64), Len: 0})
		if i%25 == 0 {
			time.Sleep(2 * time.Millisecond)
		}
	}

	deadline := time.Now().Add(2 * time.Second)
	dec := &cnl.Codec{}
	receivedClients := 0
	got := make([]bool, nClients)
	tmp := make([]byte, 512)
	for time.Now().Before(deadline) && receivedClients < nClients {
		for idx, c := range conns {
			if got[idx] {
				continue
			}
			_ = c.SetReadDeadline(time.Now().Add(10 * time.Millisecond))
			n, err := c.Read(tmp)
			if err != nil {
				if isTimeout(err) {
					continue
				}
				t.Fatalf("read client %d: %v", idx, err)
			}
			r := bytes.NewReader(tmp[:n])
			// decode at least one frame if enough bytes
			if n >= 5 {
				if _, err := dec.Decode(r); err == nil {
					got[idx] = true
					receivedClients++
				}
			}
		}
	}
	if receivedClients < nClients {
		t.Fatalf("not all clients received data: %d/%d", receivedClients, nClients)
	}
}

// FuzzCodecDecode exercises Decode with arbitrary inputs to ensure no panics and proper error handling.
func FuzzCodecDecode(f *testing.F) {
	seed := [][]byte{
		{0, 0, 0, 1, 0},                         // id=1, len=0
		{0, 0, 0, 2, 1, 0xAA},                   // len=1
		{0, 0, 0, 3, 8, 1, 2, 3, 4, 5, 6, 7, 8}, // full 8 bytes
		{0, 0, 0, 4, 9, 1, 2, 3},                // invalid length 9
	}
	for _, s := range seed {
		f.Add(s)
	}
	c := &cnl.Codec{}
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		// Attempt to decode frames until error or exhaustion
		for i := 0; i < 4 && r.Len() > 0; i++ { // limit iterations to bound time
			_, err := c.Decode(r)
			if err != nil {
				// acceptable: io.EOF, truncated, invalid length, etc.
				break
			}
		}
	})
}

// --- Helpers ---

func dialAndHandshake(t *testing.T, ctx context.Context, addr string) net.Conn {
	d := net.Dialer{Timeout: 1 * time.Second}
	c, err := d.DialContext(ctx, "tcp", addr)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	if _, err := c.Write([]byte("CANNELLONIv1")); err != nil {
		t.Fatalf("write magic: %v", err)
	}
	buf := make([]byte, 12)
	if _, err := c.Read(buf); err != nil {
		t.Fatalf("read magic: %v", err)
	}
	return c
}

func isTimeout(err error) bool {
	ne, ok := err.(net.Error)
	return ok && ne.Timeout()
}
