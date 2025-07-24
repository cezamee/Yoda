// Utility functions for CPU affinity and NUMA topology management
// Fonctions utilitaires pour la gestion de l'affinité CPU et la topologie NUMA
package main

import (
	"fmt"
	"runtime"

	"golang.org/x/sys/unix"
)

// Set CPU affinity for optimal performance
// Définit l'affinité CPU pour des performances optimales
func setCPUAffinity(cpuCore int) error {
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

	fmt.Printf("🎯 Pinned goroutine to CPU core %d (TID: %d)\n", cpuCore, tid)
	return nil
}

// Detect NUMA topology and provide basic awareness info
// Détecte la topologie NUMA et fournit des informations de base
func detectNUMATopology() {
	numCPU := runtime.NumCPU()
	fmt.Printf("🔍 System topology: %d CPU cores detected\n", numCPU)

	if numCPU >= 4 {
		fmt.Printf("✅ CPU affinity optimization: Ideal configuration (4+ cores)\n")
		fmt.Printf("   - Core %d: RX processing\n", cpuRXProcessing)
		fmt.Printf("   - Core %d: TX processing\n", cpuTXProcessing)
		fmt.Printf("   - Core %d: TLS crypto\n", cpuTLSCrypto)
		fmt.Printf("   - Core %d: PTY I/O\n", cpuPTYIO)
	} else if numCPU >= 2 {
		fmt.Printf("⚠️ CPU affinity optimization: Limited cores (%d), may have contention\n", numCPU)
	} else {
		fmt.Printf("⚠️ CPU affinity optimization: Single core detected, affinity disabled\n")
	}
}
