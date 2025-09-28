package cnl

import (
	"bytes"
	"testing"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

func benchmarkFrames(n int) []can.Frame {
	frames := make([]can.Frame, n)
	for i := range frames {
		frames[i] = mkFrame(uint32(0x500+i), 8)
	}
	return frames
}

func BenchmarkCodec_Encode_64(b *testing.B) {
	c := Codec{}
	frs := benchmarkFrames(64)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = c.Encode(frs)
	}
}

func BenchmarkCodec_EncodeTo_64(b *testing.B) {
	c := Codec{}
	frs := benchmarkFrames(64)
	var buf bytes.Buffer
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = c.EncodeTo(&buf, frs)
	}
}

func BenchmarkCodec_DecodeN_64(b *testing.B) {
	c := Codec{}
	frs := benchmarkFrames(64)
	wire := c.Encode(frs)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(wire)
		_, _ = c.DecodeN(r, 0, func(can.Frame) {})
	}
}
