//go:build linux

package socketcan

import (
	"encoding/binary"
	"fmt"
	"net"

	"golang.org/x/sys/unix"

	"github.com/kstaniek/go-ampio-server/internal/can"
)

type Device struct {
	fd int
}

func Open(iface string) (*Device, error) {
	fd, err := unix.Socket(unix.AF_CAN, unix.SOCK_RAW, unix.CAN_RAW)
	if err != nil {
		return nil, fmt.Errorf("socket(AF_CAN): %w", err)
	}
	if err := unix.SetsockoptInt(fd, unix.SOL_CAN_RAW, unix.CAN_RAW_FD_FRAMES, 0); err != nil {
		// Older kernels may not know this option; ignore ENOPROTOOPT
		if err != unix.ENOPROTOOPT {
			_ = unix.Close(fd)
			return nil, fmt.Errorf("disable CAN FD: %w", err)
		}
	}
	ifi, err := net.InterfaceByName(iface)
	if err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("if %q: %w", iface, err)
	}
	sa := &unix.SockaddrCAN{Ifindex: ifi.Index}
	if err := unix.Bind(fd, sa); err != nil {
		_ = unix.Close(fd)
		return nil, fmt.Errorf("bind(can@%s): %w", iface, err)
	}
	return &Device{fd: fd}, nil
}

func (d *Device) Close() error { return unix.Close(d.fd) }

// ReadFrame reads one classic CAN frame from the raw CAN socket.
func (d *Device) ReadFrame(fr *can.Frame) error {
	var buf [unix.CAN_MTU]byte // classic CAN MTU = 16 bytes
	n, err := unix.Read(d.fd, buf[:])
	if err != nil {
		return err
	}
	if n != unix.CAN_MTU {
		return fmt.Errorf("short read: %d", n)
	}

	// struct can_frame (linux/can.h):
	//   can_id  u32   [0:4]  (includes EFF/RTR/ERR flags)
	//   can_dlc u8    [4]
	//   pad     3B    [5:8]
	//   data    [8]   [8:16]
	//
	// NOTE: The kernel provides fields in host byte order. On common Linux
	// archs (little-endian) this matches binary.LittleEndian. If you ever
	// target big-endian, switch to BigEndian here.
	id := binary.LittleEndian.Uint32(buf[0:4])
	dlc := int(buf[4])
	if dlc < 0 || dlc > 8 {
		dlc = 8
	}

	fr.CANID = id
	fr.Len = uint8(dlc)
	copy(fr.Data[:], buf[8:8+dlc])
	return nil
}

// WriteFrame writes one classic CAN frame to the raw CAN socket.
func (d *Device) WriteFrame(fr can.Frame) error {
	var buf [unix.CAN_MTU]byte
	binary.LittleEndian.PutUint32(buf[0:4], fr.CANID)
	buf[4] = fr.Len
	copy(buf[8:], fr.Data[:fr.Len])
	_, err := unix.Write(d.fd, buf[:])
	return err
}
