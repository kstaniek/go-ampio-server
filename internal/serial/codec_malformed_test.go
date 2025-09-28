package serial

import (
	"bytes"
	"testing"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
)

// TestDecodeStreamMalformed ensures malformed length / checksum increment metric.
func TestDecodeStreamMalformed(t *testing.T) {
	var buf bytes.Buffer
	codec := Codec{}
	before := metrics.Snap().Malformed

	// Build a valid small frame then corrupt checksum.
	data := []byte{0, 0, 0, 1, 0xAA} // ID + 1B payload
	frame := canUARTSend(data)       // returns preamble 2D D4 len checksum
	frame[len(frame)-1] ^= 0xFF      // corrupt checksum
	buf.Write(frame)
	if err := codec.DecodeStream(&buf, func(_ can.Frame) {}); err != nil {
		t.Fatalf("DecodeStream error: %v", err)
	}
	after := metrics.Snap().Malformed
	if after <= before {
		t.Fatalf("expected malformed metric increment, before=%d after=%d", before, after)
	}
}
