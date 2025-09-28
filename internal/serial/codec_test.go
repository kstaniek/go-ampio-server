package serial

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

// build an RX-wire frame: data := ID(4) | PAYLOAD(0..8), then envelope with 2D D4 LEN ... CRC
func rxWire(id uint32, payload []byte) []byte {
	rawID := id & can.CAN_EFF_MASK
	data := make([]byte, 4+len(payload))
	binary.BigEndian.PutUint32(data[:4], rawID)
	copy(data[4:], payload)
	return canUARTSend(data)
}

func f(id uint32, data ...byte) can.Frame {
	var fr can.Frame
	fr.CANID = (id & can.CAN_EFF_MASK) | can.CAN_EFF_FLAG
	fr.Len = uint8(len(data))
	copy(fr.Data[:], data)
	return fr
}

func TestSerialCodec_RoundTrip_Chunked(t *testing.T) {
	codec := Codec{}

	// NOTE: all DLC > 0 to match current DecodeStream minLn (no DLC=0 on RX)
	want := []can.Frame{
		f(0x0001E5A, 0x34, 0x7B, 0x70, 0xD7, 0x94, 0x10, 0x0D, 0xF7), // 8B
		f(0x0001F55, 0xA1, 0xB2, 0xC3, 0xD4, 0xE5, 0xF6),             // 6B
		f(0x0123456, 0x9A, 0xBC),                                     // 2B
		f(0x01ABCDE, 0xDE, 0xAD, 0xBE),                               // 3B
	}

	// Build a continuous RX stream (ID|PAYLOAD wrapped in UART envelope)
	stream := make([]byte, 0, 512)
	for _, fr := range want {
		stream = append(stream, rxWire(fr.CANID, fr.Data[:fr.Len])...)
	}

	var buf bytes.Buffer
	got := make([]can.Frame, 0, len(want))

	// Feed in irregular small chunks to stress preamble alignment & partials.
	chunkSizes := []int{1, 2, 3, 4, 5, 7, 11}
	cs := 0
	for pos := 0; pos < len(stream); {
		n := chunkSizes[cs%len(chunkSizes)]
		cs++
		if pos+n > len(stream) {
			n = len(stream) - pos
		}
		buf.Write(stream[pos : pos+n])
		pos += n

		if err := codec.DecodeStream(&buf, func(fr can.Frame) {
			got = append(got, fr.CopyShallow())
		}); err != nil {
			t.Fatalf("DecodeStream error: %v", err)
		}
	}

	if len(got) != len(want) {
		t.Fatalf("decoded %d frames, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].CANID != want[i].CANID ||
			got[i].Len != want[i].Len ||
			string(got[i].Data[:got[i].Len]) != string(want[i].Data[:want[i].Len]) {
			t.Fatalf("frame %d mismatch\n got  id=0x%X len=%d data=% X\n want id=0x%X len=%d data=% X",
				i,
				got[i].CANID, got[i].Len, got[i].Data[:got[i].Len],
				want[i].CANID, want[i].Len, want[i].Data[:want[i].Len])
		}
	}
}
