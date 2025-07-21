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

// eBPF statistics display utility
// Utilitaire d'affichage des statistiques eBPF
package main

import (
	"fmt"
)

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
		if err := b.statsMap.Lookup(&key, &perCPUValues); err != nil {
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
		stats[0], tcpListenPort, stats[1], tcpListenPort, stats[2], stats[3])
}
