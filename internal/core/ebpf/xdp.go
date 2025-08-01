// XDP initialization and configuration utilities
// Utilitaires pour l'initialisation et la configuration XDP
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

// initializeXDP loads and configures all XDP components for the given interface
// initializeXDP charge et configure tous les composants XDP pour l'interface donnée
func InitializeXDP(interfaceName string) (*ebpf.Collection, *ebpf.Program, *ebpf.Map, *ebpf.Map, *xdp.ControlBlock, link.Link, []byte, uint32) {
	queueID := uint32(0)

	// Get network interface by name
	// Récupère l'interface réseau par son nom
	ifi, err := net.InterfaceByName(interfaceName)
	if err != nil {
		log.Fatalf("Failed to get interface %s: %v", interfaceName, err)
	}

	// Load eBPF program from object file
	// Charge le programme eBPF depuis le fichier objet
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(xdpObj))
	if err != nil {
		log.Fatalf("Failed to load eBPF program: %v", err)
	}
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		log.Fatalf("Failed to create eBPF collection: %v", err)
	}

	// Get XDP program and maps from collection
	// Récupère le programme XDP et les maps de la collection
	prog := coll.Programs["xdp_redirect_port"]
	if prog == nil {
		log.Fatalf("XDP program not found")
	}
	xsksMap := coll.Maps["xsks_map"]
	statsMap := coll.Maps["stats_map"]

	// Configure AF_XDP socket options
	// Configure les options du socket AF_XDP
	opts := xdp.DefaultOpts()
	opts.NFrames = 4096            // Number of frames / Nombre de trames
	opts.FrameSize = cfg.FrameSize // Frame size / Taille des trames
	opts.NDescriptors = 2048       // Number of descriptors / Nombre de descripteurs
	opts.Bind = true               // Bind socket / Lie le socket
	opts.UseNeedWakeup = true      // Enable need_wakeup / Active need_wakeup

	// Create XDP socket
	// Crée le socket XDP
	cb, err := xdp.New(uint32(ifi.Index), queueID, opts)
	if err != nil {
		log.Fatalf("Failed to create XDP socket: %v", err)
	}

	// Insert socket FD into XSKMAP
	// Insère le FD du socket dans la XSKMAP
	socketFD := cb.UMEM.SockFD()
	if err := xsksMap.Update(queueID, socketFD, ebpf.UpdateAny); err != nil {
		log.Fatalf("Failed to insert socket into XSKMAP: %v", err)
	}

	// Attach XDP program to interface
	// Attache le programme XDP à l'interface
	l, err := link.AttachXDP(link.XDPOptions{
		Program:   prog,
		Interface: ifi.Index,
		Flags:     link.XDPDriverMode,
	})
	if err != nil {
		// Fallback to generic mode if driver mode fails
		// Utilise le mode générique si le mode driver échoue
		l, err = link.AttachXDP(link.XDPOptions{
			Program:   prog,
			Interface: ifi.Index,
			Flags:     link.XDPGenericMode,
		})
		if err != nil {
			log.Fatalf("Failed to attach XDP: %v", err)
		}
	}

	// Get source MAC address
	// Récupère l'adresse MAC source
	var srcMAC []byte
	if len(ifi.HardwareAddr) == 6 {
		srcMAC = make([]byte, 6)
		copy(srcMAC, ifi.HardwareAddr)
	} else {
		// Default MAC if not available
		// MAC par défaut si non disponible
		srcMAC = []byte{0x02, 0x00, 0x00, 0x00, 0x00, 0x01}
	}
	// Return all initialized XDP components
	// Retourne tous les composants XDP initialisés
	return coll, prog, xsksMap, statsMap, cb, l, srcMAC, queueID
}
