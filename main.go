// MIT License
// Copyright (c) 2025 Cezame
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in all
// copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

/*
Grogu TLS AF_XDP Network Server main entrypoint
Entrée principale du serveur réseau Grogu TLS AF_XDP

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
	"fmt"
	"log"
	"os"
	"os/signal"
	"runtime"
	"syscall"

	"github.com/cilium/ebpf/rlimit"
)

func main() {
	// Remove memlock limit for eBPF usage
	// Supprime la limite memlock pour l'utilisation d'eBPF
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("Failed to remove memlock: %v", err)
	}

	// Detect system topology for CPU affinity optimization
	// Détecte la topologie système pour optimiser l'affinité CPU
	fmt.Printf("🚀 Initializing Grogu TLS AF_XDP Reverse Shell with CPU Affinity Optimization\n")
	detectNUMATopology()

	// Initialize XDP components
	// Initialise les composants XDP
	coll, _, _, statsMap, cb, l, srcMAC, queueID := initializeXDP(interfaceName)
	defer coll.Close()
	defer l.Close()

	// Create Gvisor netstack
	// Crée la netstack Gvisor
	netstackStack, linkEP := createNetstack()

	// Create bridge with correct structure
	// Crée le bridge avec la bonne structure
	bridge := &NetstackBridge{
		cb:       cb,
		queueID:  queueID,
		stack:    netstackStack,
		linkEP:   linkEP,
		statsMap: statsMap,
		srcMAC:   srcMAC,
	}

	// Signal handling
	// Gestion des signaux
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)

	// Start goroutines with dedicated CPU cores for performance
	// Démarre les goroutines sur des cœurs CPU dédiés pour la performance
	fmt.Printf("🎯 Starting performance-optimized goroutines with CPU affinity...\n")

	// RX Processing on dedicated CPU core
	// Traitement RX sur un cœur CPU dédié
	go func() {
		if runtime.NumCPU() >= 4 {
			if err := setCPUAffinity(cpuRXProcessing); err != nil {
				fmt.Printf("⚠️ CPU affinity for RX processing failed: %v\n", err)
			}
		}
		bridge.startPacketProcessing()
	}()

	// TLS Server on dedicated CPU core for crypto operations
	// Serveur TLS sur un cœur CPU dédié pour les opérations cryptographiques
	go func() {
		if runtime.NumCPU() >= 4 {
			if err := setCPUAffinity(cpuTLSCrypto); err != nil {
				fmt.Printf("⚠️ CPU affinity for TLS crypto failed: %v\n", err)
			}
		}
		bridge.setupTCPServer()
	}()

	// Wait for termination signal
	// Attente du signal de terminaison
	<-c
}
