// Network and performance configuration constants
// Constantes de configuration réseau et performance
package cfg

import (
	"github.com/cilium/ebpf"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/xdp"
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
)

// shared structs
type NetstackBridge struct {
	Cb        *xdp.ControlBlock // XDP control block / Bloc de contrôle XDP
	QueueID   uint32            // XDP queue ID / Identifiant de file XDP
	Stack     *stack.Stack      // Gvisor netstack / Netstack Gvisor
	LinkEP    *channel.Endpoint // Netstack endpoint / Point de terminaison netstack
	StatsMap  *ebpf.Map         // eBPF stats map / Map eBPF statistiques
	ClientMAC [6]byte           // Fixed-size MAC array / Tableau MAC taille fixe
	SrcMAC    []byte            // Source MAC address / Adresse MAC source
	RxRing    *RxRingBuffer     // Typed RX ring buffer
	TxRing    *TxRingBuffer     // Typed TX ring buffer
}
type RxRingBuffer struct {
	Buf   []RxPacket
	Size  int
	Head  int
	Tail  int
	Count int
	Mask  int
}

type TxRingBuffer struct {
	Buf   [][]byte
	Size  int
	Head  int
	Tail  int
	Count int
	Mask  int
}
type RxPacket struct {
	Buffer    []byte
	FrameAddr uint64
}
