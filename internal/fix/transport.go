package fix

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"time"
)

type Transport interface {
	io.Reader
	io.Writer
	io.Closer
}

type DialFunc func(context.Context) (Transport, error)

func TLSDialer(address, serverName string, timeout time.Duration) DialFunc {
	return TLSConfigDialer(address, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName}, timeout)
}

func TLSConfigDialer(address string, tlsConfig *tls.Config, timeout time.Duration) DialFunc {
	return func(ctx context.Context) (Transport, error) {
		dialer := &net.Dialer{Timeout: timeout}
		if tlsConfig == nil {
			return nil, fmt.Errorf("FIX TLS config is required")
		}
		config := tlsConfig.Clone()
		if config.MinVersion < tls.VersionTLS12 {
			config.MinVersion = tls.VersionTLS12
		}
		conn, err := (&tls.Dialer{NetDialer: dialer, Config: config}).DialContext(ctx, "tcp", address)
		if err != nil {
			return nil, fmt.Errorf("dial FIX TLS transport: %w", err)
		}
		return conn, nil
	}
}
