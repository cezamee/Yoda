// gVisor Netstack initialization and WebSocket mTLS server setup
package core

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"

	cfg "github.com/cezamee/Yoda/internal/config"
	"github.com/cezamee/Yoda/internal/core/services"
	"github.com/gorilla/websocket"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

//go:embed certs/server.crt
var serverCertPEM []byte

//go:embed certs/server.key
var serverKeyPEM []byte

//go:embed certs/ca.crt
var caCertPEM []byte

// Create and configure the gVisor network stack (NIC, IP, routes)
func CreateNetstack() (*stack.Stack, *channel.Endpoint) {

	// Initialize stack with IPv4, TCP, UDP support
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})

	// Create virtual NIC endpoint (channel)
	linkEP := channel.New(64, cfg.NetMTU, "")

	// Register NIC with the stack
	if err := s.CreateNIC(cfg.NetNicID, linkEP); err != nil {
		log.Fatalf("Failed to create NIC: %v", err)
	}

	// Assign IPv4 address to NIC
	protocolAddr := tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   tcpip.AddrFromSlice(net.ParseIP(cfg.NetLocalIP).To4()),
			PrefixLen: 24,
		},
	}

	// Add address to NIC
	if err := s.AddProtocolAddress(cfg.NetNicID, protocolAddr, stack.AddressProperties{}); err != nil {
		log.Fatalf("Failed to add address: %v", err)
	}

	// Add default route
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			Gateway:     tcpip.AddrFromSlice(net.ParseIP(cfg.NetGateway).To4()),
			NIC:         cfg.NetNicID,
		},
	})

	// Return stack and NIC endpoint
	return s, linkEP
}

func SetupWebSocketServer(b *cfg.NetstackBridge) {

	var upgrader = websocket.Upgrader{
		ReadBufferSize:  80 * 1024,
		WriteBufferSize: 80 * 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	cert, err := tls.X509KeyPair(serverCertPEM, serverKeyPEM)
	if err != nil {
		log.Fatalf("Failed to load server cert/key: %v", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		log.Fatalf("Failed to load CA cert")
	}
	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		},
		PreferServerCipherSuites: true,
		ClientAuth:               tls.RequireAndVerifyClientCert,
		ClientCAs:                caPool,
	}

	ln, err := gonet.ListenTCP(b.Stack, tcpip.FullAddress{
		NIC:  cfg.NetNicID,
		Addr: tcpip.AddrFromSlice(net.ParseIP(cfg.NetLocalIP).To4()),
		Port: cfg.TcpListenPort,
	}, ipv4.ProtocolNumber)
	if err != nil {
		log.Fatalf("failed to create gonet listener: %v", err)
	}

	tlsListener := tls.NewListener(ln, tlsConfig)

	// Create HTTP server with WebSocket handler
	mux := http.NewServeMux()
	mux.HandleFunc("/shell", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()
		fmt.Printf("🔗 [WebSocket] Shell session started from %s\n", r.RemoteAddr)
		services.HandleWebSocketPTYSession(conn)
		fmt.Printf("📡 [WebSocket] Shell session ended from %s\n", r.RemoteAddr)
	})

	mux.HandleFunc("/download", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "Missing path parameter", http.StatusBadRequest)
			return
		}
		fmt.Printf("🔽 [HTTPS] Download request for %s from %s\n", path, r.RemoteAddr)
		http.ServeFile(w, r, path)
		fmt.Printf("📡 [HTTPS] Download session ended from %s\n", r.RemoteAddr)
	})

	mux.HandleFunc("/upload", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPut {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
			return
		}
		path := r.URL.Query().Get("path")
		if path == "" {
			http.Error(w, "Missing path parameter", http.StatusBadRequest)
			return
		}
		fmt.Printf("📤 [HTTPS] Upload request for %s from %s\n", path, r.RemoteAddr)
		if _, err := os.Stat(path); err == nil {
			http.Error(w, "File already exists", http.StatusConflict)
			fmt.Printf("❌ File already exists: %s\n", path)
			return
		}
		out, err := os.Create(path)
		if err != nil {
			http.Error(w, "Cannot create file", http.StatusInternalServerError)
			fmt.Printf("❌ Cannot create file: %v\n", err)
			return
		}
		defer out.Close()
		written, err := io.Copy(out, r.Body)
		if err != nil {
			http.Error(w, "Error writing file", http.StatusInternalServerError)
			fmt.Printf("❌ Error writing file: %v\n", err)
			return
		}
		fmt.Printf("✅ Uploaded %d bytes to %s\n", written, path)
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, "Upload successful: %d bytes\n", written)
		fmt.Printf("📡 [HTTP] Upload session ended from %s\n", r.RemoteAddr)
	})

	mux.HandleFunc("/ps", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		fmt.Printf("🔍 [WebSocket] PS session started from %s\n", r.RemoteAddr)
		services.HandleWebSocketPSSession(conn)
		fmt.Printf("📡 [WebSocket] PS session ended from %s\n", r.RemoteAddr)
	})

	mux.HandleFunc("/ls", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		fmt.Printf("📁 [WebSocket] LS session started from %s\n", r.RemoteAddr)
		services.HandleWebSocketLSSession(conn)
		fmt.Printf("📡 [WebSocket] LS session ended from %s\n", r.RemoteAddr)
	})

	mux.HandleFunc("/cat", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		fmt.Printf("📄 [WebSocket] Cat session started from %s\n", r.RemoteAddr)
		services.HandleWebSocketCatSession(conn)
		fmt.Printf("📡 [WebSocket] Cat session ended from %s\n", r.RemoteAddr)
	})

	mux.HandleFunc("/rm", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		fmt.Printf("🗑️ [WebSocket] Rm session started from %s\n", r.RemoteAddr)
		services.HandleWebSocketRmSession(conn)
		fmt.Printf("📡 [WebSocket] Rm session ended from %s\n", r.RemoteAddr)
	})

	httpServer := &http.Server{
		Handler:   mux,
		TLSConfig: tlsConfig,
	}

	fmt.Printf("✅ [WebSocket] ready on %s:%d (mTLS)\n", cfg.NetLocalIP, cfg.TcpListenPort)
	if err := httpServer.Serve(tlsListener); err != nil {
		log.Fatalf("WebSocket server error: %v", err)
	}
}
