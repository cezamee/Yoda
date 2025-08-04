// gVisor Netstack initialization and WebSocket mTLS server setup
// Initialisation du Netstack gVisor et configuration du serveur WebSocket mTLS
package core

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"log"
	"net"
	"net/http"

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
// Crée et configure le Netstack gVisor (NIC, IP, routes)
func CreateNetstack() (*stack.Stack, *channel.Endpoint) {

	// Initialize stack with IPv4, TCP, UDP support
	// Initialise la stack avec support IPv4, TCP, UDP
	s := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})

	// Create virtual NIC endpoint (channel)
	// Crée un endpoint NIC virtuel (channel)
	linkEP := channel.New(8192, cfg.NetMTU, "")

	// Register NIC with the stack
	// Enregistre le NIC dans la stack
	if err := s.CreateNIC(cfg.NetNicID, linkEP); err != nil {
		log.Fatalf("Failed to create NIC: %v", err)
	}

	// Assign IPv4 address to NIC
	// Assigne une adresse IPv4 au NIC
	protocolAddr := tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   tcpip.AddrFromSlice(net.ParseIP(cfg.NetLocalIP).To4()),
			PrefixLen: 24,
		},
	}

	// Add address to NIC
	// Ajoute l'adresse au NIC
	if err := s.AddProtocolAddress(cfg.NetNicID, protocolAddr, stack.AddressProperties{}); err != nil {
		log.Fatalf("Failed to add address: %v", err)
	}

	// Add default route (gateway)
	// Ajoute la route par défaut (gateway)
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			Gateway:     tcpip.AddrFromSlice(net.ParseIP(cfg.NetGateway).To4()),
			NIC:         cfg.NetNicID,
		},
	})

	// Return stack and NIC endpoint
	// Retourne la stack et l'endpoint NIC
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
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Printf("WebSocket upgrade failed: %v", err)
			return
		}
		defer conn.Close()

		fmt.Printf("🔽 [WebSocket] Download session started from %s\n", r.RemoteAddr)
		services.HandleWebSocketDownload(conn)
		fmt.Printf("📡 [WebSocket] Download session ended from %s\n", r.RemoteAddr)
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
