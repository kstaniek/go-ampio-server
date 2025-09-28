package cnl

import (
	"bytes"
	"io"
	"testing"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

// TestDecodeN_MultiFrame verifies DecodeN drains multiple frames from a single buffer.
func TestDecodeN_MultiFrame(t *testing.T) {
	c := Codec{}
	in := []can.Frame{mkFrame(0x10, 8), mkFrame(0x11, 5), mkFrame(0x12, 0)}
	buf := bytes.NewReader(c.Encode(in))
	var out []can.Frame
	n, err := c.DecodeN(buf, 0, func(f can.Frame) { out = append(out, f.CopyShallow()) })
	if err != io.EOF && err != nil { // EOF expected at clean end
		t.Fatalf("DecodeN err=%v", err)
	}
	if n != len(in) || len(out) != len(in) {
		t.Fatalf("decoded %d collected %d want %d", n, len(out), len(in))
	}
	for i := range in {
		if out[i].CANID != in[i].CANID || out[i].Len != in[i].Len {
			t.Fatalf("frame %d mismatch", i)
		}
	}
}
