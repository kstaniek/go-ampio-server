package cnl

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

const hello = "CANNELLONIv1"

func Handshake(ctx context.Context, c net.Conn, timeout time.Duration) error {
	if deadlineErr := c.SetDeadline(time.Now().Add(timeout)); deadlineErr != nil {
		return fmt.Errorf("set deadline: %w", deadlineErr)
	}
	defer c.SetDeadline(time.Time{})

	errCh := make(chan error, 2)

	// Writer
	go func() {
		_, err := io.WriteString(c, hello)
		errCh <- err
	}()

	// Reader
	go func() {
		buf := make([]byte, len(hello))
		_, err := io.ReadFull(c, buf)
		if err == nil && string(buf) != hello {
			err = errors.New("bad hello")
		}
		errCh <- err
	}()

	// Wait for both operations or context cancel
	for i := 0; i < 2; i++ {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case err := <-errCh:
			if err != nil {
				return fmt.Errorf("handshake: %w", err)
			}
		}
	}
	return nil
}
