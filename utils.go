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
