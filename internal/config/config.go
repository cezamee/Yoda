// Network and performance configuration constants
// Constantes de configuration réseau et performance
package cfg

import (
	"gvisor.dev/gvisor/pkg/tcpip"
)

// File or directory name prefixes to hide
var HiddenPrefixes = []string{"secret_", "hidden_"}

const (
	// Network interface and IP configuration
	// Configuration de l'interface réseau et IP
	NetNicID   = tcpip.NICID(1) // NIC identifier / Identifiant NIC
	NetLocalIP = "192.168.0.38" // Local IP address / Adresse IP locale
	NetGateway = "192.168.0.1"  // Gateway IP address / Adresse IP de la passerelle
	NetMTU     = 1500           // MTU size / Taille MTU

	// Packet processing parameters
	// Paramètres de traitement des paquets
	EthHeaderSize   = 14        // Ethernet header size / Taille en-tête Ethernet
	IpHeaderMinSize = 20        // Minimum IP header size / Taille minimale en-tête IP
	FrameSize       = 2048      // Frame size / Taille de trame
	InterfaceName   = "enp46s0" // Network interface name / Nom de l'interface réseau

	TcpListenPort = 443 // TCP listen port / Port d'écoute TCP
	UdpListenPort = 443 // UDP listen port / Port d'écoute UDP

	// CPU affinity for optimal performance
	// Affinité CPU pour performance optimale
	CpuRXProcessing = 0 // RX packet processing core / Cœur RX
	CpuTXProcessing = 1 // TX packet processing core / Cœur TX
	CpuTLSCrypto    = 2 // TLS/crypto operations core / Cœur TLS/crypto
	CpuPTYIO        = 3 // PTY I/O operations core / Cœur PTY I/O
)
