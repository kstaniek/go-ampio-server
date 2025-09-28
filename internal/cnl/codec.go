package cnl

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"

	"github.com/kstaniek/go-ampio-server/internal/metrics"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

// Codec encodes/decodes cannelloni frames. Stateless and safe for concurrent use.
type Codec struct{}

// ErrInvalidLength is returned when a frame length (DLC) is outside 0..8.
var ErrInvalidLength = errors.New("cannelloni: invalid length")

// ErrTruncatedFrame is returned when the underlying reader ends mid-frame.
var ErrTruncatedFrame = errors.New("cannelloni: truncated frame")

// Encode packs frames into a single cannelloni packet (DATA).
func (c *Codec) Encode(frames []can.Frame) []byte {
	if len(frames) == 0 {
		return nil
	}
	var buf bytes.Buffer
	// Pre-size: worst case per frame = 4(id)+1(len)+8(data)
	buf.Grow(len(frames) * (4 + 1 + 8))
	_, _ = c.EncodeTo(&buf, frames)
	return buf.Bytes()
}

// EncodeTo writes the wire representation of frames to w and returns bytes written.
// Each frame is encoded as: 4-byte BE CANID, 1-byte length (lower 7 bits), payload.
func (c *Codec) EncodeTo(w io.Writer, frames []can.Frame) (int, error) {
	var total int
	for _, f := range frames {
		var id [4]byte
		binary.BigEndian.PutUint32(id[:], f.CANID)
		n, err := w.Write(id[:])
		total += n
		if err != nil {
			return total, fmt.Errorf("cannelloni encode id: %w", err)
		}
		if _, err := w.Write([]byte{f.Len}); err != nil { // length byte
			total++ // conservative increment
			return total, fmt.Errorf("cannelloni encode len: %w", err)
		}
		ln := int(f.Len & 0x7F)
		if ln > 0 {
			n, err = w.Write(f.Data[:ln])
			total += n
			if err != nil {
				return total, fmt.Errorf("cannelloni encode data: %w", err)
			}
		}
	}
	return total, nil
}

// Decode reads exactly one frame from r.
// It returns io.EOF if called at a clean frame boundary and no more data is available.
func (c *Codec) Decode(r io.Reader) (can.Frame, error) {
	var f can.Frame
	var idb [4]byte
	if _, err := io.ReadFull(r, idb[:]); err != nil {
		return f, err
	}
	f.CANID = binary.BigEndian.Uint32(idb[:])
	// Read one length byte; treat 0 bytes read as EOF
	var lb [1]byte
	n, err := r.Read(lb[:])
	if err != nil {
		return f, err
	}
	if n == 0 {
		return f, io.EOF
	}
	ln := int(lb[0] & 0x7F) // high bit masked per protocol (future flags?)
	if ln > 8 {             // ln cannot be negative
		metrics.IncMalformed()
		return f, fmt.Errorf("cannelloni decode: %w (%d)", ErrInvalidLength, ln)
	}
	f.Len = uint8(ln)
	if ln > 0 {
		if _, err := io.ReadFull(r, f.Data[:ln]); err != nil {
			if errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
				metrics.IncMalformed()
				return f, fmt.Errorf("cannelloni decode payload: %w", ErrTruncatedFrame)
			}
			metrics.IncMalformed()
			return f, fmt.Errorf("cannelloni decode payload: %w", err)
		}
	}
	return f, nil
}

// DecodeN decodes up to max frames (if max>0) or until EOF (if max<=0) invoking onFrame for each.
// It returns the number of frames decoded and the terminal error (which can be io.EOF).
func (c *Codec) DecodeN(r io.Reader, max int, onFrame func(can.Frame)) (int, error) {
	var n int
	for max <= 0 || n < max {
		fr, err := c.Decode(r)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return n, err
			}
			return n, err
		}
		onFrame(fr)
		n++
	}
	return n, nil
}

// DecodeStream kept for backward compatibility with earlier tests; decodes a single frame.
func (c *Codec) DecodeStream(r io.Reader, onFrame func(can.Frame)) error {
	fr, err := c.Decode(r)
	if err != nil {
		return err
	}
	onFrame(fr)
	return nil
}
