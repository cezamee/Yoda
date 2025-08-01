// Utility functions for CPU affinity and NUMA topology management
// Fonctions utilitaires pour la gestion de l'affinité CPU et la topologie NUMA
package core

import (
	"fmt"
	"runtime"

	cfg "github.com/cezamee/Yoda/internal/config"
	"golang.org/x/sys/unix"
)

// Set CPU affinity for optimal performance
// Définit l'affinité CPU pour des performances optimales
func SetCPUAffinity(cpuCore int) error {
	runtime.LockOSThread()

	numCPU := runtime.NumCPU()
	if cpuCore >= numCPU {
		fmt.Printf("⚠️ CPU core %d not available (max: %d), using core 0\n", cpuCore, numCPU-1)
		cpuCore = 0
	}

	var cpuSet unix.CPUSet
	cpuSet.Zero()
	cpuSet.Set(cpuCore)

	tid := unix.Gettid()
	if err := unix.SchedSetaffinity(tid, &cpuSet); err != nil {
		return fmt.Errorf("failed to set CPU affinity to core %d: %v", cpuCore, err)
	}
	return nil
}

// Detect NUMA topology and provide basic awareness info
// Détecte la topologie NUMA et fournit des informations de base
func DetectNUMATopology() {
	numCPU := runtime.NumCPU()

	if numCPU >= 4 {
		fmt.Printf("✅ CPU affinity optimization: Ideal configuration (4+ cores)\n")
	} else if numCPU >= 2 {
		fmt.Printf("⚠️ CPU affinity optimization: Limited cores (%d), may have contention\n", numCPU)
	} else {
		fmt.Printf("⚠️ CPU affinity optimization: Single core detected, affinity disabled\n")
	}
}

// printStats displays eBPF statistics for the NetstackBridge
// printStats affiche les statistiques eBPF pour NetstackBridge
func (b *NetstackBridge) printStats() {
	var stats [4]uint64

	// For a PERCPU_ARRAY map, read all per-CPU values and sum them
	// Pour une map PERCPU_ARRAY, lire toutes les valeurs par CPU et les additionner
	for i := 0; i < 4; i++ {
		key := uint32(i)

		// Read values from all CPUs / Lire les valeurs de tous les CPUs
		var perCPUValues []uint64
		if err := b.StatsMap.Lookup(&key, &perCPUValues); err != nil {
			fmt.Printf("⚠️ Failed to read stats[%d]: %v\n", i, err)
			stats[i] = 0
			continue
		}

		// Sum all per-CPU values / Additionner toutes les valeurs par CPU
		var total uint64
		for _, value := range perCPUValues {
			total += value
		}
		stats[i] = total
	}

	// Print formatted statistics / Affiche les statistiques formatées
	fmt.Printf("📊 Stats - Total: %d, TCP %d : %d, UDP %d: %d, Redirected: %d\n",
		stats[0], cfg.TcpListenPort, stats[1], cfg.UdpListenPort, stats[2], stats[3])
}
