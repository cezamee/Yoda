package main

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"net/url"

	cfg "github.com/cezamee/Yoda/internal/config"
	"github.com/gorilla/websocket"
)

//go:embed certs/ca.crt
var caCertPEM []byte

//go:embed certs/client.crt
var clientCertPEM []byte

//go:embed certs/client.key
var clientKeyPEM []byte

// createSecureWebSocketConnection creates a secure WebSocket connection with mutual TLS
func createSecureWebSocketConnection(path string) (*websocket.Conn, error) {
	cert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		return nil, fmt.Errorf("failed to load client cert/key: %v", err)
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		return nil, fmt.Errorf("failed to load CA cert")
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
	}

	dialer := websocket.Dialer{
		TLSClientConfig: tlsConfig,
	}

	wsURL := url.URL{
		Scheme: "wss",
		Host:   fmt.Sprintf("%s:%d", cfg.NetLocalIP, cfg.TcpListenPort),
		Path:   path,
	}

	conn, _, err := dialer.Dial(wsURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("WebSocket connection failed: %v", err)
	}

	return conn, nil
}
