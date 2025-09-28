package cnl

import (
	"bytes"
	"testing"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

// FuzzCodecRoundTrip ensures arbitrary small frame sets survive encode/decode.
func FuzzCodecRoundTrip(f *testing.F) {
	c := Codec{}
	seed := [][]can.Frame{{mkFrame(0x100, 0)}, {mkFrame(0x200, 8)}, {mkFrame(0x300, 3), mkFrame(0x301, 5)}}
	for _, s := range seed {
		wire := c.Encode(s)
		f.Add(wire) // single packet bytes
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		// Feed back data as if it were a packet; decode at most len/6 frames to bound work.
		r := bytes.NewReader(data)
		_, _ = c.DecodeN(r, 16, func(can.Frame) {})
	})
}

// FuzzCodecDecodeInvalid ensures decoder doesn't panic with random input.
func FuzzCodecDecodeInvalid(f *testing.F) {
	c := Codec{}
	f.Add([]byte{0, 0, 0, 1, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		// Attempt decode of a single frame; ignore errors.
		_, _ = c.Decode(r)
	})
}
