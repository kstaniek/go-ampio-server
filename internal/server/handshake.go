package server

import (
	"context"
	"net"

	"github.com/kstaniek/go-ampio-server/internal/cnl"
)

// CannelloniHandshake runs the required TCP hello exchange.
func (s *Server) CannelloniHandshake(ctx context.Context, c net.Conn) error {
	return cnl.Handshake(ctx, c, s.handshakeTimeout)
}
