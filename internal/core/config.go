// Network and performance configuration constants
// Constantes de configuration réseau et performance
package core

import (
	"gvisor.dev/gvisor/pkg/tcpip"
)

const (
	// Network interface and IP configuration
	// Configuration de l'interface réseau et IP
	netNicID   = tcpip.NICID(1) // NIC identifier / Identifiant NIC
	netLocalIP = "192.168.0.38" // Local IP address / Adresse IP locale
	netMTU     = 1500           // MTU size / Taille MTU

	// Packet processing parameters
	// Paramètres de traitement des paquets
	ethHeaderSize   = 14        // Ethernet header size / Taille en-tête Ethernet
	ipHeaderMinSize = 20        // Minimum IP header size / Taille minimale en-tête IP
	frameSize       = 2048      // Frame size / Taille de trame
	InterfaceName   = "enp46s0" // Network interface name / Nom de l'interface réseau

	ptyBufferSize = 16384 // PTY read buffer (16KB) / Buffer de lecture PTY (16Ko)
	txBatchSize   = 32    // TX batch size / Taille du lot TX
	tcpListenPort = 443   // TCP listen port / Port d'écoute TCP

	// CPU affinity for optimal performance
	// Affinité CPU pour performance optimale
	CpuRXProcessing = 0 // RX packet processing core / Cœur RX
	CpuTXProcessing = 1 // TX packet processing core / Cœur TX
	CpuTLSCrypto    = 2 // TLS/crypto operations core / Cœur TLS/crypto
	CpuPTYIO        = 3 // PTY I/O operations core / Cœur PTY I/O
)
