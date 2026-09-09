package fix

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"testing"
	"time"
)

func TestTLSConfigDialerUsesVerifiedTLS12OrNewer(t *testing.T) {
	certificate, roots := testCertificate(t)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	serverDone := make(chan error, 1)
	go func() {
		conn, err := listener.Accept()
		if err != nil {
			serverDone <- err
			return
		}
		defer conn.Close()
		buffer := make([]byte, 4)
		_, err = io.ReadFull(conn, buffer)
		if err == nil && string(buffer) != "ping" {
			err = io.ErrUnexpectedEOF
		}
		serverDone <- err
	}()
	dial := TLSConfigDialer(listener.Addr().String(), &tls.Config{ServerName: "localhost", RootCAs: roots}, time.Second)
	transport, err := dial(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	connection, ok := transport.(*tls.Conn)
	if !ok {
		t.Fatalf("transport type %T", transport)
	}
	if err := connection.Handshake(); err != nil {
		t.Fatal(err)
	}
	if connection.ConnectionState().Version < tls.VersionTLS12 {
		t.Fatalf("TLS version=%x", connection.ConnectionState().Version)
	}
	if _, err := transport.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	_ = transport.Close()
	if err := <-serverDone; err != nil {
		t.Fatal(err)
	}
}

func TestTLSConfigDialerRejectsWrongHostname(t *testing.T) {
	certificate, roots := testCertificate(t)
	listener, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{certificate}})
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			defer conn.Close()
			if tlsConn, ok := conn.(*tls.Conn); ok {
				_ = tlsConn.Handshake()
			}
		}
	}()
	_, err = TLSConfigDialer(listener.Addr().String(), &tls.Config{ServerName: "wrong.invalid", RootCAs: roots}, time.Second)(context.Background())
	if err == nil {
		t.Fatal("wrong TLS hostname accepted")
	}
}

func testCertificate(t *testing.T) (tls.Certificate, *x509.CertPool) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "localhost"}, DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate := tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}
	roots := x509.NewCertPool()
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(parsed)
	return certificate, roots
}
