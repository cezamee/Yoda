package main

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"io"
	"net/http"
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

func createSecureHTTPClient(method, query string, body io.Reader) (*http.Response, error) {
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
	transport := &http.Transport{TLSClientConfig: tlsConfig}
	client := &http.Client{Transport: transport}

	url := fmt.Sprintf("https://%s:%d%s", cfg.NetLocalIP, cfg.TcpListenPort, query)

	var req *http.Request
	switch method {
	case http.MethodGet:
		req, err = http.NewRequest(http.MethodGet, url, nil)
	case http.MethodPut:
		if body == nil {
			return nil, fmt.Errorf("PUT method requires a non-nil body")
		}
		req, err = http.NewRequest(http.MethodPut, url, body)
	default:
		return nil, fmt.Errorf("unsupported HTTP method: %s", method)
	}
	if err != nil {
		return nil, fmt.Errorf("HTTP request creation failed: %v", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %v", err)
	}
	return resp, nil
}
