// Package net provides utilities for creating secure WebSocket and HTTP connections.
package net

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	cfg "github.com/cezamee/Yoda/internal/config"
	"github.com/gorilla/websocket"
)

//go:embed certs/ca.crt
var caCertPEM []byte

//go:embed certs/client.crt
var clientCertPEM []byte

//go:embed certs/client.key
var clientKeyPEM []byte

func CreateSecureWebSocketConnection(path string) (*websocket.Conn, error) {
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
		Host:   fmt.Sprintf("%s:%d", cfg.CliTargetIP, cfg.TcpListenPort),
		Path:   path,
	}

	conn, _, err := dialer.Dial(wsURL.String(), nil)
	if err != nil {
		return nil, fmt.Errorf("WebSocket connection failed: %v", err)
	}

	return conn, nil
}

func CreateSecureHTTPClient(method, query string, body io.Reader) (*http.Response, error) {
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

	url := fmt.Sprintf("https://%s:%d%s", cfg.CliTargetIP, cfg.TcpListenPort, query)

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

type ProgressWriter struct {
	Out          io.Writer
	Total        *int64
	Size         int64
	StartTime    time.Time
	LastPrint    *time.Time
	ShowProgress bool
}

func (pw *ProgressWriter) Write(p []byte) (int, error) {
	n, err := pw.Out.Write(p)
	*pw.Total += int64(n)
	if pw.ShowProgress && time.Since(*pw.LastPrint) > 500*time.Millisecond {
		percent := float64(*pw.Total) / float64(pw.Size)
		elapsed := time.Since(pw.StartTime).Seconds()
		if elapsed <= 0 {
			elapsed = 1e-3
		}
		speed := float64(*pw.Total) / (1024 * 1024) / elapsed
		fmt.Printf("\r%.0f%% - %.2f MB/s", percent*100, speed)
		*pw.LastPrint = time.Now()
	}
	return n, err
}
