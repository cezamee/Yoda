// AF_XDP packet processing and optimization utilities
// Utilitaires pour le traitement de paquets et optimisation AF_XDP
package main

import (
	"context"
	"fmt"
	"runtime"
	"time"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

var (
	fallbackDestMAC     = []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	etherTypeIPv4       = []byte{0x08, 0x00}
	prebuiltEtherHeader = make([]byte, ethHeaderSize)
)

func init() {
	copy(prebuiltEtherHeader[0:6], fallbackDestMAC)
	copy(prebuiltEtherHeader[12:14], etherTypeIPv4)
}

// Start main AF_XDP packet processing loop
// Démarre la boucle principale de traitement de paquets AF_XDP
func (b *NetstackBridge) startPacketProcessing() {
	fmt.Printf("🔄 Starting packet processing with proper AF_XDP queue management...\n")

	b.cb.UMEM.Lock()
	b.cb.Fill.FillAll(&b.cb.UMEM)
	b.cb.UMEM.Unlock()
	fmt.Printf("📋 Fill Queue initialized\n")

	go func() {
		if runtime.NumCPU() >= 4 {
			if err := setCPUAffinity(cpuTXProcessing); err != nil {
				fmt.Printf("⚠️ CPU affinity for TX processing failed: %v\n", err)
			}
		}
		b.handleOutboundPackets()
	}()

	statsTicker := time.NewTicker(10 * time.Second)
	defer statsTicker.Stop()

	// Adaptive sleep: start small, increase if no work
	// Sommeil adaptatif : commence court, augmente si pas de travail
	sleepDuration := 10 * time.Microsecond
	maxSleep := 100 * time.Microsecond

	// Main processing loop with proper queue management
	// Boucle principale de traitement avec gestion correcte des files
	for {
		workDone := false

		select {
		case <-statsTicker.C:
			b.printStats()
		default:
			// STEP 1: Process TX completion queue FIRST
			// Étape 1 : traiter la file de complétion TX en premier
			if b.processCompletionQueue() {
				workDone = true
			}

			// STEP 2: Process incoming RX packets
			// Étape 2 : traiter les paquets RX entrants
			if b.processRXQueue() {
				workDone = true
			}

			// STEP 3: Maintain Fill queue (ensure RX can continue)
			// Étape 3 : maintenir la file Fill (assure la continuité RX)
			b.maintainFillQueue()

			// Adaptive sleep: sleep less when busy, more when idle
			// Sommeil adaptatif : moins quand occupé, plus quand inactif
			if workDone {
				sleepDuration = 10 * time.Microsecond
			} else {
				if sleepDuration < maxSleep {
					sleepDuration += 10 * time.Microsecond
				}
			}
			time.Sleep(sleepDuration)
		}
	}
}

// Process TX completion queue
// Traite la file de complétion TX
func (b *NetstackBridge) processCompletionQueue() bool {
	b.cb.UMEM.Lock()
	nCompleted, completionIndex := b.cb.Completion.Peek()
	if nCompleted > 0 {
		completedFrames := make([]uint64, nCompleted)
		for i := uint32(0); i < nCompleted; i++ {
			completedFrames[i] = b.cb.Completion.Get(completionIndex + i)
		}
		b.cb.Completion.Release(nCompleted)
		for _, frameAddr := range completedFrames {
			b.cb.UMEM.FreeFrame(frameAddr)
		}
		b.cb.UMEM.Unlock()
		return true
	}
	b.cb.UMEM.Unlock()
	return false
}

// Process RX queue
// Traite la file RX
func (b *NetstackBridge) processRXQueue() bool {
	b.cb.UMEM.Lock()
	nReceived, index := b.cb.RX.Peek()
	if nReceived == 0 {
		b.cb.UMEM.Unlock()
		return false
	}

	type rxPacket struct {
		buffer    []byte // direct UMEM buffer
		frameAddr uint64
	}

	packets := make([]rxPacket, nReceived)
	for i := uint32(0); i < nReceived; i++ {
		desc := b.cb.RX.Get(index + i)
		packetData := b.cb.UMEM.Get(desc)
		packets[i] = rxPacket{
			buffer:    packetData, // direct UMEM buffer
			frameAddr: uint64(desc.Addr),
		}
	}

	b.cb.RX.Release(nReceived)
	b.cb.UMEM.Unlock()

	// Phase 2: Process packets WITHOUT holding UMEM lock
	for _, pkt := range packets {
		b.processPacket(pkt.buffer)
	}

	// Phase 3: Free RX frames directly
	b.cb.UMEM.Lock()
	for _, pkt := range packets {
		b.cb.UMEM.FreeFrame(pkt.frameAddr)
	}
	b.cb.UMEM.Unlock()

	return true
}

func (b *NetstackBridge) maintainFillQueue() {
	b.cb.UMEM.Lock()
	b.cb.Fill.FillAll(&b.cb.UMEM)
	b.cb.UMEM.Unlock()
}

// --- TX Batch definitions ---
type txBatchEntry struct {
	data []byte
}

var txBatchBuf [txBatchSize]txBatchEntry
var txBatchCount int

// flushTXBatch sends all packets in the batch and resets the batch
func (b *NetstackBridge) flushTXBatch() {
	if txBatchCount == 0 {
		return
	}
	for i := 0; i < txBatchCount; i++ {
		b.sendPacketTX(txBatchBuf[i].data)
		txBatchBuf[i].data = nil
	}
	txBatchCount = 0
}

func (b *NetstackBridge) handleOutboundPackets() {
	fmt.Printf("🚀 Starting event-driven outbound packet handler...\n")

	ctx := context.Background()
	flushInterval := 50 * time.Microsecond
	flushTicker := time.NewTicker(flushInterval)
	defer flushTicker.Stop()

	for {
		select {
		case <-flushTicker.C:
			if txBatchCount > 0 {
				b.flushTXBatch()
			}
		default:
			pkt := b.linkEP.ReadContext(ctx)
			if pkt == nil {
				// Flush any remaining batch on end-of-stream or idle
				b.flushTXBatch()
				fmt.Printf("📡 ReadContext returned nil, checking termination...\n")
				continue
			}

			data := pkt.ToView().AsSlice()
			// Add to batch
			txBatchBuf[txBatchCount] = txBatchEntry{data: data}
			txBatchCount++
			if txBatchCount >= txBatchSize {
				b.flushTXBatch()
			}
			pkt.DecRef()
		}
	}
}

func (b *NetstackBridge) sendPacketTX(ipData []byte) {
	if len(ipData) < ipHeaderMinSize {
		return
	}

	b.cb.UMEM.Lock()
	defer b.cb.UMEM.Unlock()

	// FIRST: Process completion queue to free up sent frames (batch operation)
	nCompleted, completionIndex := b.cb.Completion.Peek()
	if nCompleted > 0 {
		completedFrames := make([]uint64, nCompleted)
		for i := uint32(0); i < nCompleted; i++ {
			completedFrames[i] = b.cb.Completion.Get(completionIndex + i)
		}
		b.cb.Completion.Release(nCompleted)

		for _, frameAddr := range completedFrames {
			b.cb.UMEM.FreeFrame(frameAddr)
		}
	}

	// SECOND: Try to reserve a TX descriptor
	nReserved, index := b.cb.TX.Reserve(&b.cb.UMEM, 1)
	if nReserved == 0 {
		nCompleted, completionIndex := b.cb.Completion.Peek()
		if nCompleted > 0 {
			completedFrames := make([]uint64, nCompleted)
			for i := uint32(0); i < nCompleted; i++ {
				completedFrames[i] = b.cb.Completion.Get(completionIndex + i)
			}
			b.cb.Completion.Release(nCompleted)
			for _, frameAddr := range completedFrames {
				b.cb.UMEM.FreeFrame(frameAddr)
			}
			// Retry reservation
			nReserved, index = b.cb.TX.Reserve(&b.cb.UMEM, 1)
		}

		if nReserved == 0 {
			return
		}
	}

	// THIRD: Get free frame for packet
	frameAddr := b.cb.UMEM.AllocFrame()
	if frameAddr == 0 {
		return
	}

	frame := b.cb.UMEM.Get(unix.XDPDesc{Addr: frameAddr, Len: uint32(ethHeaderSize + len(ipData))})
	if len(frame) < ethHeaderSize+len(ipData) || ethHeaderSize+len(ipData) > frameSize {
		b.cb.UMEM.FreeFrame(frameAddr)
		return
	}

	copy(frame[0:ethHeaderSize], prebuiltEtherHeader)
	if b.clientMAC != [6]byte{} {
		copy(frame[0:6], b.clientMAC[:])
	}

	copy(frame[6:12], b.srcMAC)
	copy(frame[ethHeaderSize:], ipData)

	desc := unix.XDPDesc{Addr: frameAddr, Len: uint32(ethHeaderSize + len(ipData))}
	b.cb.TX.Set(index, desc)
	b.cb.TX.Notify()
}

func (b *NetstackBridge) processPacket(packetData []byte) {
	if len(packetData) < (ethHeaderSize + ipHeaderMinSize) {
		return
	}

	if len(packetData) >= ethHeaderSize {
		copy(b.clientMAC[:], packetData[6:12])
	}

	ipPacket := packetData[ethHeaderSize:]

	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(ipPacket),
	})
	b.linkEP.InjectInbound(ipv4.ProtocolNumber, pkt)
	pkt.DecRef()
}
