//go:build !linux

package socketcan

import "errors"

// ErrTxOverflow is provided for non-linux builds so server code can compile.
var ErrTxOverflow = errors.New("socketcan tx overflow (stub)")
