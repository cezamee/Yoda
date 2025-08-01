// gVisor Netstack initialization and gRPC mTLS server setup
// Initialisation du Netstack gVisor et configuration du serveur gRPC mTLS
package core

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"log"
	"net"

	cfg "github.com/cezamee/Yoda/internal/config"
	"github.com/cezamee/Yoda/internal/core/pb"

	"google.golang.org/grpc"

	"github.com/cilium/ebpf"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/xdp"
)

//go:embed certs/server.crt
var serverCertPEM []byte

//go:embed certs/server.key
var serverKeyPEM []byte

//go:embed certs/ca.crt
var caCertPEM []byte

// NetstackBridge links XDP, netstack, and TLS components
// NetstackBridge relie les composants XDP, netstack et TLS
type NetstackBridge struct {
	Cb        *xdp.ControlBlock // XDP control block / Bloc de contrôle XDP
	QueueID   uint32            // XDP queue ID / Identifiant de file XDP
	Stack     *stack.Stack      // Gvisor netstack / Netstack Gvisor
	LinkEP    *channel.Endpoint // Netstack endpoint / Point de terminaison netstack
	StatsMap  *ebpf.Map         // eBPF stats map / Map eBPF statistiques
	ClientMAC [6]byte           // Fixed-size MAC array / Tableau MAC taille fixe
	SrcMAC    []byte            // Source MAC address / Adresse MAC source
}

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
	linkEP := channel.New(64, cfg.NetMTU, "")

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

type loggingTLSListener struct {
	net.Listener
}

func (l *loggingTLSListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err == nil {
		remoteAddr := conn.RemoteAddr()
		fmt.Printf("[gRPC] Incoming connection from %s\n", remoteAddr)
		conn = &loggingConn{Conn: conn}
	}
	return conn, err
}

type loggingConn struct {
	net.Conn
	closed bool
}

func (c *loggingConn) Close() error {
	if !c.closed {

		c.closed = true
		remoteAddr := c.RemoteAddr()
		fmt.Printf("[gRPC] Connection closed from %s\n", remoteAddr)
		c.closed = true
	}
	return c.Conn.Close()

}

func (b *NetstackBridge) SetupGRPCServer() {

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
		NextProtos:               []string{"h2"},
	}

	ln, err := gonet.ListenTCP(b.Stack, tcpip.FullAddress{
		NIC:  cfg.NetNicID,
		Addr: tcpip.AddrFromSlice(net.ParseIP(cfg.NetLocalIP).To4()),
		Port: cfg.TcpListenPort,
	}, ipv4.ProtocolNumber)
	if err != nil {
		log.Fatalf("failed to create gonet listener: %v", err)
	}

	tlsListener := &loggingTLSListener{tls.NewListener(ln, tlsConfig)}

	grpcServer := grpc.NewServer()

	// Register gRPC services
	pb.RegisterPTYShellServer(grpcServer, &PTYShellServerImpl{Bridge: b})

	fmt.Printf("✅ [gRPC] ready on %s:%d (mTLS)\n", cfg.NetLocalIP, cfg.TcpListenPort)
	if err := grpcServer.Serve(tlsListener); err != nil {
		log.Fatalf("gRPC server error: %v", err)
	}

}
