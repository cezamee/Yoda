// XDP initialization and configuration utilities
package ebpf

import (
	"bytes"
	_ "embed"
	"log"
	"net"

	cfg "github.com/cezamee/Yoda/internal/config"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
	"gvisor.dev/gvisor/pkg/xdp"
)

//go:embed obj/xdp_redirect.o
var xdpObj []byte

func InitializeXDP(interfaceName string) (*ebpf.Collection, *ebpf.Program, *ebpf.Map, *ebpf.Map, *xdp.ControlBlock, link.Link, []byte, uint32) {
	queueID := uint32(0)

	ifi, err := net.InterfaceByName(interfaceName)
	if err != nil {
		log.Fatalf("Failed to get interface %s: %v", interfaceName, err)
	}

	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(xdpObj))
	if err != nil {
		log.Fatalf("Failed to load eBPF program: %v", err)
	}
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		log.Fatalf("Failed to create eBPF collection: %v", err)
	}

	prog := coll.Programs["xdp_redirect_port"]
	if prog == nil {
		log.Fatalf("XDP program not found")
	}
	xsksMap := coll.Maps["xsks_map"]
	statsMap := coll.Maps["stats_map"]

	opts := xdp.DefaultOpts()
	opts.NFrames = 4096
	opts.FrameSize = cfg.FrameSize
	opts.NDescriptors = 2048
	opts.Bind = true
	opts.UseNeedWakeup = true

	cb, err := xdp.New(uint32(ifi.Index), queueID, opts)
	if err != nil {
		log.Fatalf("Failed to create XDP socket: %v", err)
	}

	socketFD := cb.UMEM.SockFD()
	if err := xsksMap.Update(queueID, socketFD, ebpf.UpdateAny); err != nil {
		log.Fatalf("Failed to insert socket into XSKMAP: %v", err)
	}

	l, err := link.AttachXDP(link.XDPOptions{
		Program:   prog,
		Interface: ifi.Index,
		Flags:     link.XDPDriverMode,
	})
	if err != nil {
		l, err = link.AttachXDP(link.XDPOptions{
			Program:   prog,
			Interface: ifi.Index,
			Flags:     link.XDPGenericMode,
		})
		if err != nil {
			log.Fatalf("Failed to attach XDP: %v", err)
		}
	}

	var srcMAC []byte
	if len(ifi.HardwareAddr) == 6 {
		srcMAC = make([]byte, 6)
		copy(srcMAC, ifi.HardwareAddr)
	} else {
		srcMAC = []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}
	return coll, prog, xsksMap, statsMap, cb, l, srcMAC, queueID
}
