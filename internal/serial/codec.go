package serial

import (
	"bytes"
	"encoding/binary"

	"github.com/kstaniek/go-ampio-server/internal/can"
	"github.com/kstaniek/go-ampio-server/internal/metrics"
)

type Codec struct{}

// CompactBuffer reclaims consumed prefix capacity when underlying buffer
// grows too large relative to unread bytes. It returns true if compaction
// occurred. Thresholds chosen to avoid excessive copying.
func CompactBuffer(b *bytes.Buffer) bool {
	data := b.Bytes()
	// If buffer size < 1KB, skip.
	if len(data) < 1024 {
		return false
	}
	// If unread < 25% of capacity, compact.
	if cap(data) > 0 && len(data)*4 < cap(data) {
		clone := make([]byte, len(data))
		copy(clone, data)
		b.Reset()
		_, _ = b.Write(clone)
		return true
	}
	return false
}

// canUARTSend builds a UART frame:
// [0x2D, 0xD4, len+1, data..., checksum]
// checksum = (len+1) + 0x2D + sum(data) (mod 256)
func canUARTSend(data []byte) []byte {
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

func (Codec) Encode(f can.Frame) []byte {
	can_id := f.CANID
	if f.CANID&can.CAN_EFF_FLAG != 0 {
		can_id &= can.CAN_EFF_MASK
	}
	tab := make([]byte, 6+f.Len) // INS(1) + FLAGS(1) + ID(4) + PAYLOAD(0..8)
	tab[0] = 2                   // INS: 2 = CAN UART SEND WITH EXT ID
	tab[1] = 0x80 + f.Len        // FLAGS/DLC (0x80 | len) for classic
	tab[2] = byte(can_id >> 24)
	tab[3] = byte(can_id >> 16)
	tab[4] = byte(can_id >> 8)
	tab[5] = byte(can_id)
	for i, b := range f.Data[:f.Len] {
		tab[6+i] = b
	}
	return canUARTSend(tab[:6+f.Len])
}

// DecodeStream reads from in and emits complete frames via out.
// It returns nil if no error occurred (including io.EOF).
//
// Example frame (DLC=8):
// 2D D3 - preamble
// 0D    - len = 13 = can_id(4) + payload(8) + checksum(1)
// 00 00 00 02 - CAN ID = 0x00000002
// FE 10 19 09 19 04 01 20 - payload (8 bytes)
// AA    - checksum = 0x2D + len + sum(data bytes after len)
//
// Example frame (DLC=2):
// 2D D4 0D 00 00 00 02 FE 10 19 09 19 04 01 20 AA
func (Codec) DecodeStream(in *bytes.Buffer, out func(can.Frame)) error {
	const (
		pre0 = 0x2D
		pre1 = 0xD4

		// ln = dataBytes + 1(checksum)
		// dataBytes = INS(1) + FLAGS(1) + ID(4) + PAYLOAD(0..8)
		minLn = 6 + 0 + 1 // 7 -> allow DLC=0 (zero-length payload)
		maxLn = 6 + 8 + 1 // 15 -> allow DLC up to 8
	)
	header := []byte{pre0, pre1}

	for {
		data := in.Bytes()
		// Periodically compact to avoid unbounded growth from misaligned garbage
		_ = CompactBuffer(in)
		if len(data) < 3 { // need preamble + len
			return nil
		}

		// align to preamble
		i := bytes.Index(data, header)
		if i < 0 {
			// keep last byte in case next buffer starts with preamble second byte
			if in.Len() > 1 {
				last := data[len(data)-1]
				in.Reset()
				_ = in.WriteByte(last)
			}
			return nil
		}
		if i > 0 {
			in.Next(i)
			continue
		}

		// preamble at start; need length
		if len(data) < 4 {
			return nil
		}
		ln := int(data[2]) // includes (data bytes + 1 checksum)
		if ln < minLn || ln > maxLn {
			// malformed length; advance one byte to resync
			metrics.IncMalformed()
			in.Next(1)
			continue
		}

		req := 3 + ln // total bytes: 2 preamble + 1 len + ln
		if len(data) < req {
			return nil
		}

		// checksum: 0x2D + len + sum(data bytes after len)
		sum := uint(pre0) + uint(data[2])
		for _, b := range data[3 : req-1] {
			sum += uint(b)
		}
		if byte(sum) != data[req-1] {
			// checksum mismatch: count and attempt resync
			metrics.IncMalformed()
			in.Next(1)
			continue
		}

		// parse frame
		id := binary.BigEndian.Uint32(data[3:7])
		payload := data[7 : req-1] // length can be 0..8

		var f can.Frame
		f.CANID = id | can.CAN_EFF_FLAG
		f.Len = uint8(len(payload))
		copy(f.Data[:], payload)

		out(f)
		metrics.IncSerialRx()
		in.Next(req)
	}
}
