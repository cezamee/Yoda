package main

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"

	cfg "github.com/cezamee/Yoda/internal/config"
	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

//go:embed certs/ca.crt
var caCertPEM []byte

//go:embed certs/client.crt
var clientCertPEM []byte

//go:embed certs/client.key
var clientKeyPEM []byte

// Message structure for WebSocket communication
type WSMessage struct {
	Type string `json:"type"` // "data", "resize"
	Data []byte `json:"data,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Cols int    `json:"cols,omitempty"`
}

// runShellSession starts an interactive shell session using WebSocket streaming.
func runShellSession() {
	fmt.Println("🔗 Connecting to shell...")

	// Setup TLS config for mutual TLS
	cert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		log.Printf("Failed to load client cert/key: %v", err)
		return
	}

	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		log.Printf("Failed to load CA cert")
		return
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
		Path:   "/shell",
	}

	conn, _, err := dialer.Dial(wsURL.String(), nil)
	if err != nil {
		log.Printf("WebSocket connection failed: %v", err)
		return
	}
	defer conn.Close()

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Printf("Failed to set raw mode: %v", err)
		return
	}
	defer func() {
		term.Restore(int(os.Stdin.Fd()), oldState)
		fmt.Print("\033[2J\033[H")
	}()

	// Send terminal size
	if width, height, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		fmt.Printf("📐 Terminal size: %dx%d\n", width, height)
		resizeMsg := WSMessage{
			Type: "resize",
			Rows: height,
			Cols: width,
		}
		msgBytes, _ := json.Marshal(resizeMsg)
		conn.WriteMessage(websocket.TextMessage, msgBytes)
	}

	fmt.Println("✅ Connected! Type 'exit' or press Ctrl+D to return to CLI")

	done := make(chan bool, 1)
	inputDone := make(chan bool, 1)

	// stdin -> WebSocket
	go func() {
		defer func() { inputDone <- true }()
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				// Check for Ctrl+D (EOF)
				if n == 1 && buf[0] == 4 {
					done <- true
					return
				}
				msg := WSMessage{
					Type: "data",
					Data: buf[:n],
				}
				msgBytes, err := json.Marshal(msg)
				if err != nil {
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
					return
				}
			}
		}
	}()

	// WebSocket -> stdout
	go func() {
		defer func() { done <- true }()
		for {
			_, msgBytes, err := conn.ReadMessage()
			if err != nil {
				return
			}
			var msg WSMessage
			if err := json.Unmarshal(msgBytes, &msg); err != nil {
				continue
			}
			if msg.Type == "data" && len(msg.Data) > 0 {
				os.Stdout.Write(msg.Data)
			}
		}
	}()

	// Wait for session to end
	select {
	case <-done:
	case <-inputDone:
	}
}
