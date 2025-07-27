// gVisor Netstack initialization and TCP/TLS server setup
// Initialisation du Netstack gVisor et configuration du serveur TCP/TLS
package core

import (
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"runtime"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

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
	linkEP := channel.New(64, netMTU, "")

	// Register NIC with the stack
	// Enregistre le NIC dans la stack
	if err := s.CreateNIC(netNicID, linkEP); err != nil {
		log.Fatalf("Failed to create NIC: %v", err)
	}

	// Assign IPv4 address to NIC
	// Assigne une adresse IPv4 au NIC
	protocolAddr := tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{
			Address:   tcpip.AddrFromSlice(net.ParseIP(netLocalIP).To4()),
			PrefixLen: 24,
		},
	}

	// Add address to NIC
	// Ajoute l'adresse au NIC
	if err := s.AddProtocolAddress(netNicID, protocolAddr, stack.AddressProperties{}); err != nil {
		log.Fatalf("Failed to add address: %v", err)
	}

	// Add default route (gateway)
	// Ajoute la route par défaut (gateway)
	s.SetRouteTable([]tcpip.Route{
		{
			Destination: header.IPv4EmptySubnet,
			Gateway:     tcpip.AddrFromSlice(net.ParseIP(netGateway).To4()),
			NIC:         netNicID,
		},
	})

	// Return stack and NIC endpoint
	// Retourne la stack et l'endpoint NIC
	return s, linkEP
}

func (b *NetstackBridge) SetupTCPServer() {
	fmt.Printf("🔧 Setting up TLS PTY Reverse Shell server...\n")

	// Generate self-signed certificate
	cert, err := generateSelfSignedCert()
	if err != nil {
		log.Fatalf("Failed to generate certificate: %v", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		ServerName:   "Yoda",
		MinVersion:   tls.VersionTLS12,
		MaxVersion:   tls.VersionTLS12,
		CipherSuites: []uint16{
			tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
			tls.TLS_RSA_WITH_AES_128_GCM_SHA256,
			tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
		},
		PreferServerCipherSuites: true,
		InsecureSkipVerify:       false,
		ClientAuth:               tls.NoClientCert,
	}

	fwd := tcp.NewForwarder(b.Stack, 0, 256, func(r *tcp.ForwarderRequest) {
		if r.ID().LocalPort != tcpListenPort {
			r.Complete(true)
			return
		}

		// Create session key from remote endpoint for logging only
		sessionKey := fmt.Sprintf("%s:%d", r.ID().RemoteAddress, r.ID().RemotePort)
		fmt.Printf("🎯 New TLS PTY reverse shell connection from %s!\n", sessionKey)

		var wq waiter.Queue
		ep, err := r.CreateEndpoint(&wq)
		if err != nil {
			fmt.Printf("❌ Failed to create endpoint: %v\n", err)
			r.Complete(true)
			return
		}

		r.Complete(false)

		go func() {
			defer func() {
				ep.Close()
				fmt.Printf("📡 Session %s cleaned up\n", sessionKey)
			}()

			// CPU Affinity Optimization: Pin TLS session to PTY I/O core
			if runtime.NumCPU() >= 4 {
				if err := SetCPUAffinity(CpuPTYIO); err != nil {
					fmt.Printf("⚠️ CPU affinity for PTY I/O failed: %v\n", err)
				}
			}

			gonetConn := gonet.NewTCPConn(&wq, ep)

			tlsConn := tls.Server(gonetConn, tlsConfig)

			fmt.Printf("🔐 Starting TLS handshake for %s...\n", sessionKey)
			err := tlsConn.Handshake()
			if err != nil {
				fmt.Printf("❌ TLS handshake failed for %s: %v\n", sessionKey, err)
				gonetConn.Close()
				return
			}

			fmt.Printf("✅ TLS connection established for %s\n", sessionKey)

			b.handleTLSPTYSession(tlsConn, sessionKey)
		}()
	})

	b.Stack.SetTransportProtocolHandler(tcp.ProtocolNumber, fwd.HandlePacket)
	fmt.Printf("✅ TLS PTY reverse shell server ready\n")
}
