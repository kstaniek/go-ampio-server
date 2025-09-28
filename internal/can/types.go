package can

// SocketCAN flag bits for can_id (same values as <linux/can.h>)
const (
	CAN_EFF_FLAG = 0x80000000
	CAN_RTR_FLAG = 0x40000000
	CAN_ERR_FLAG = 0x20000000
	CAN_SFF_MASK = 0x7FF
	CAN_EFF_MASK = 0x1FFFFFFF
)

// Frame is a simple CAN/ frame holder used across the gateway.
// can_id contains EFF/RTR/ERR flags in its upper bits like SocketCAN.
// Len is payload length (0..8 for classic); only the first Len bytes are valid.
//
// Note: This is a convenience type. Codecs map this to/from their wires.
type Frame struct {
	CANID uint32
	Len   uint8
	Data  [64]byte
}

func (f Frame) CopyShallow() Frame { // handy for tests
	var g Frame
	g.CANID, g.Len = f.CANID, f.Len
	copy(g.Data[:], f.Data[:])
	return g
}
