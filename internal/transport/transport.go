package transport

import (
	"io"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/cnl"
)

// FrameDecoder decodes a single CAN frame from a stream.
type FrameDecoder interface {
	Decode(r io.Reader) (can.Frame, error)
}

// MultiFrameDecoder optionally drains multiple frames from a stream.
type MultiFrameDecoder interface {
	DecodeN(r io.Reader, max int, onFrame func(can.Frame)) (int, error)
}

// FrameBatchEncoder can encode batches efficiently (either to bytes or directly to writer).
type FrameBatchEncoder interface {
	Encode([]can.Frame) []byte
	EncodeTo(w io.Writer, frames []can.Frame) (int, error)
}

// FrameSink is a generic CAN frame transmission target.
type FrameSink interface {
	SendFrame(can.Frame) error
}

// Compile-time assertions that *cnl.Codec satisfies the optional capabilities.
var (
	_ FrameDecoder      = (*cnl.Codec)(nil)
	_ MultiFrameDecoder = (*cnl.Codec)(nil)
	_ FrameBatchEncoder = (*cnl.Codec)(nil)
)
