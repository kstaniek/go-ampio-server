package cnl

import (
	"bytes"
	"crypto/rand"
	"io"
	"testing"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

func mkFrame(id uint32, n int) can.Frame {
	var f can.Frame
	f.CANID = (id & can.CAN_EFF_MASK) | can.CAN_EFF_FLAG
	if n < 0 {
		n = 0
	}
	if n > 8 {
		n = 8
	}
	f.Len = uint8(n)
	rand.Read(f.Data[:n])
	return f
}

func TestCNLCodec_RoundTrip(t *testing.T) {
	codec := Codec{}
	in := []can.Frame{
		mkFrame(0x1E5A, 8),
		mkFrame(0x1F55, 6),
		mkFrame(0x12345, 0),
	}

	wire := codec.Encode(in)
	var out []can.Frame
	// Use DecodeN over the full buffer
	br := bytes.NewReader(wire)
	n, err := codec.DecodeN(br, 0, func(f can.Frame) { out = append(out, f.CopyShallow()) })
	if err != io.EOF && err != nil { // expect EOF at clean end
		t.Fatalf("DecodeN unexpected err: %v", err)
	}
	if n != len(in) {
		t.Fatalf("decoded %d, want %d", n, len(in))
	}
	if len(out) != len(in) {
		t.Fatalf("collected %d, want %d", len(out), len(in))
	}
	for i := range in {
		if out[i].CANID != in[i].CANID || out[i].Len != in[i].Len || string(out[i].Data[:out[i].Len]) != string(in[i].Data[:in[i].Len]) {
			t.Fatalf("frame %d mismatch", i)
		}
	}
}

func TestCNLCodec_EncodeToMatchesEncode(t *testing.T) {
	codec := Codec{}
	frames := []can.Frame{mkFrame(0x10, 8), mkFrame(0x11, 3)}
	a := codec.Encode(frames)
	var buf bytes.Buffer
	if _, err := codec.EncodeTo(&buf, frames); err != nil {
		t.Fatalf("EncodeTo error: %v", err)
	}
	if !bytes.Equal(a, buf.Bytes()) {
		t.Fatalf("Encode vs EncodeTo mismatch\nenc=% X\nencTo=% X", a, buf.Bytes())
	}
}

func TestCNLCodec_DecodeErrors(t *testing.T) {
	codec := Codec{}
	// Invalid length ( >8 ) => craft payload with len=0x89
	var bad bytes.Buffer
	// id
	bad.Write([]byte{0, 0, 0, 1})
	bad.WriteByte(0x89) // length high bit masked -> 0x09 => 9 (>8)
	if _, err := codec.Decode(&bad); err == nil {
		t.Fatalf("expected error for invalid length")
	}

	// Truncated payload
	var trunc bytes.Buffer
	trunc.Write([]byte{0, 0, 0, 2})
	trunc.WriteByte(0x05)        // length 5
	trunc.Write([]byte{1, 2, 3}) // only 3 bytes instead of 5
	if _, err := codec.Decode(&trunc); err == nil {
		t.Fatalf("expected truncated error")
	}
}

func BenchmarkCNLCodec_Encode(b *testing.B) {
	codec := Codec{}
	frames := make([]can.Frame, 64)
	for i := range frames {
		frames[i] = mkFrame(uint32(0x100+i), 8)
	}
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = codec.Encode(frames)
	}
}

func BenchmarkCNLCodec_EncodeTo(b *testing.B) {
	codec := Codec{}
	frames := make([]can.Frame, 64)
	for i := range frames {
		frames[i] = mkFrame(uint32(0x200+i), 8)
	}
	var buf bytes.Buffer
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		buf.Reset()
		_, _ = codec.EncodeTo(&buf, frames)
	}
}

func BenchmarkCNLCodec_DecodeN(b *testing.B) {
	codec := Codec{}
	frames := make([]can.Frame, 64)
	for i := range frames {
		frames[i] = mkFrame(uint32(0x300+i), 8)
	}
	wire := codec.Encode(frames)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		r := bytes.NewReader(wire)
		_, _ = codec.DecodeN(r, 0, func(can.Frame) {})
	}
}
