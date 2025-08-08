// Network and performance configuration constants
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
	NetNicID    = tcpip.NICID(1) // NIC identifier
	NetLocalIP  = "192.168.0.38" // Local IP address
	NetGateway  = "192.168.0.1"  // Gateway IP address
	CliTargetIP = "192.168.0.38" // Target IP used by CLI
	NetMTU      = 1500           // MTU size

	// Packet processing parameters
	EthHeaderSize   = 14        // Ethernet header size
	IpHeaderMinSize = 20        // Minimum IP header size
	FrameSize       = 2048      // Frame size
	InterfaceName   = "enp46s0" // Network interface name

	TcpListenPort = 443 // TCP listen port
	UdpListenPort = 443 // UDP listen port
)

// shared structs
type NetstackBridge struct {
	Cb        *xdp.ControlBlock // XDP control block
	QueueID   uint32            // XDP queue ID
	Stack     *stack.Stack      // Gvisor netstack
	LinkEP    *channel.Endpoint // Netstack endpoint
	StatsMap  *ebpf.Map         // eBPF stats map
	ClientMAC [6]byte           // Fixed-size MAC array
	SrcMAC    []byte            // Source MAC address
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
