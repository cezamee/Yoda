// Utility functions
package core

import (
	"fmt"

	cfg "github.com/cezamee/Yoda/internal/config"
)

// printStats displays eBPF statistics for the NetstackBridge
func printStats(b *cfg.NetstackBridge) {
	var stats [4]uint64

	for i := 0; i < 4; i++ {
		key := uint32(i)

		var perCPUValues []uint64
		if err := b.StatsMap.Lookup(&key, &perCPUValues); err != nil {
			fmt.Printf("⚠️ Failed to read stats[%d]: %v\n", i, err)
			stats[i] = 0
			continue
		}

		var total uint64
		for _, value := range perCPUValues {
			total += value
		}
		stats[i] = total
	}

	fmt.Printf("📊 Stats - Total: %d, TCP %d : %d, UDP %d: %d, Redirected: %d\n",
		stats[0], cfg.TcpListenPort, stats[1], cfg.UdpListenPort, stats[2], stats[3])
}
