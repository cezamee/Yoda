// eBPF statistics display utility
// Utilitaire d'affichage des statistiques eBPF
package core

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
		stats[0], tcpListenPort, stats[1], tcpListenPort, stats[2], stats[3])
}
