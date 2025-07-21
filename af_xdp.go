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

// AF_XDP packet processing and optimization utilities
// Utilitaires pour le traitement de paquets et optimisation AF_XDP
package main

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

// Cached fallback MACs to avoid repeated allocations
// MAC de secours en cache pour éviter les allocations répétées
var (
	fallbackDestMAC = []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	// Pre-computed ethernet header values for faster packet building
	// Valeurs d'en-tête Ethernet pré-calculées pour accélérer la construction des paquets
	etherTypeIPv4 = []byte{0x08, 0x00}
)

// RX buffer pool - Pre-allocated buffers for RX packets
// Pool de buffers RX - buffers pré-alloués pour les paquets RX
var rxBufferPool = sync.Pool{
	New: func() any {
		buf := make([]byte, frameSize) // Pre-allocate
		return &buf
	},
}

// Lock-free completion tracking ring buffer
// Anneau lock-free pour suivi des frames complétées
type lockFreeCompletionRing struct {
	ring       [4096]uint64
	writeIndex uint64
	readIndex  uint64
}

// Pool for full packets (ethernet + IP) to avoid repeated allocations
// Pool pour paquets complets (ethernet + IP) pour éviter les allocations répétées
var packetPool = sync.Pool{
	New: func() any {
		buf := make([]byte, 0, frameSize)
		return &buf
	},
}

// Start main AF_XDP packet processing loop
// Démarre la boucle principale de traitement de paquets AF_XDP
func (b *NetstackBridge) startPacketProcessing() {
	fmt.Printf("🔄 Starting packet processing with proper AF_XDP queue management...\n")

	// Initial Fill queue population
	// Remplissage initial de la file Fill
	b.cb.UMEM.Lock()
	b.cb.Fill.FillAll(&b.cb.UMEM)
	b.cb.UMEM.Unlock()
	fmt.Printf("📋 Fill Queue initialized\n")

	// Start outbound packet handler with CPU affinity for TX processing
	// Démarre le handler de paquets sortants avec affinité CPU pour le TX
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
	workDone := false

	// FIRST: Drain lock-free completion ring
	// Premièrement : vider l'anneau lock-free
	completedFromRing := b.completionRing.DrainAll()
	if len(completedFromRing) > 0 {

		b.cb.UMEM.Lock()
		for _, frameAddr := range completedFromRing {
			b.cb.UMEM.FreeFrame(frameAddr)
		}
		b.cb.UMEM.Unlock()
		workDone = true
	}

	// SECOND: Process traditional completion queue
	// Deuxièmement : traiter la file de complétion classique
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
		workDone = true
	}
	b.cb.UMEM.Unlock()

	return workDone
}

// Process RX queue
// Traite la file RX
func (b *NetstackBridge) processRXQueue() bool {
	// Phase 1: Quick peek and batch collection
	// Phase 1 : peek rapide et collecte en batch
	b.cb.UMEM.Lock()
	nReceived, index := b.cb.RX.Peek()
	if nReceived == 0 {
		b.cb.UMEM.Unlock()
		return false
	}

	type rxPacket struct {
		bufferPtr *[]byte
		dataLen   int
		frameAddr uint64
	}

	packets := make([]rxPacket, nReceived)
	for i := uint32(0); i < nReceived; i++ {
		desc := b.cb.RX.Get(index + i)
		packetData := b.cb.UMEM.Get(desc)

		poolBufferPtr := rxBufferPool.Get().(*[]byte)
		poolBuffer := *poolBufferPtr
		dataLen := len(packetData)

		copy(poolBuffer[:dataLen], packetData)

		packets[i] = rxPacket{
			bufferPtr: poolBufferPtr,
			dataLen:   dataLen,
			frameAddr: uint64(desc.Addr),
		}
	}

	// Release RX queue immediately / libère la file RX immédiatement
	b.cb.RX.Release(nReceived)
	b.cb.UMEM.Unlock()

	// Phase 2: Process packets WITHOUT holding UMEM lock
	// Phase 2 : traite les paquets SANS garder le lock UMEM
	for _, pkt := range packets {
		buffer := *pkt.bufferPtr
		b.processPacket(buffer[:pkt.dataLen])
		rxBufferPool.Put(pkt.bufferPtr)
	}

	// Phase 3: Use lock-free completion ring for frame freeing.
	// Phase 3 : utilise l'anneau lock-free pour libérer les frames.
	for _, pkt := range packets {
		if !b.completionRing.Add(pkt.frameAddr) {

			b.cb.UMEM.Lock()
			b.cb.UMEM.FreeFrame(pkt.frameAddr)
			b.cb.UMEM.Unlock()
		}
	}

	return true
}

func (b *NetstackBridge) maintainFillQueue() {
	b.cb.UMEM.Lock()
	b.cb.Fill.FillAll(&b.cb.UMEM)
	b.cb.UMEM.Unlock()
}

func (b *NetstackBridge) handleOutboundPackets() {
	fmt.Printf("🚀 Starting event-driven outbound packet handler...\n")

	ctx := context.Background()

	for {
		pkt := b.linkEP.ReadContext(ctx)
		if pkt == nil {
			fmt.Printf("📡 ReadContext returned nil, checking termination...\n")
			continue
		}

		data := pkt.ToView().AsSlice()
		b.sendPacketTX(data)
		pkt.DecRef()
	}
}

func (b *NetstackBridge) sendPacketTX(ipData []byte) {
	fullPacketPtr := packetPool.Get().(*[]byte)
	fullPacket := (*fullPacketPtr)[:0]
	defer func() {
		packetPool.Put(fullPacketPtr)
	}()

	// Parse IP header to get destination IP for proper MAC addressing
	if len(ipData) < ipHeaderMinSize {
		return
	}

	fullPacket = fullPacket[:ethHeaderSize]

	// Use stored client MAC as destination
	if b.clientMAC != [6]byte{} {
		copy(fullPacket[0:6], b.clientMAC[:])
	} else {
		copy(fullPacket[0:6], fallbackDestMAC)
	}

	copy(fullPacket[6:12], b.srcMAC)
	copy(fullPacket[12:14], etherTypeIPv4)

	fullPacket = append(fullPacket, ipData...)

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
		// TX queue full - one retry with completion processing
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

	if len(fullPacket) > frameSize {
		b.cb.UMEM.FreeFrame(frameAddr)
		return
	}

	// FOURTH: Copy packet directly to UMEM frame (single copy)
	desc := unix.XDPDesc{Addr: frameAddr, Len: uint32(len(fullPacket))}
	frame := b.cb.UMEM.Get(desc)
	copy(frame, fullPacket)

	// Set TX descriptor at the reserved index
	b.cb.TX.Set(index, desc)

	// FIFTH: Notify kernel to send
	b.cb.TX.Notify()
}

func (b *NetstackBridge) processPacket(packetData []byte) {
	if len(packetData) < (ethHeaderSize + ipHeaderMinSize) {
		return
	}

	// Store client MAC for later use in responses
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

// Add a completed frame address to the ring
// Ajoute une adresse de frame complétée dans l'anneau
func (r *lockFreeCompletionRing) Add(frameAddr uint64) bool {
	writeIdx := atomic.LoadUint64(&r.writeIndex)
	readIdx := atomic.LoadUint64(&r.readIndex)

	if writeIdx-readIdx >= 4095 {
		return false
	}

	if atomic.CompareAndSwapUint64(&r.writeIndex, writeIdx, writeIdx+1) {
		r.ring[writeIdx&4095] = frameAddr
		return true
	}
	return false
}

// Drain all completed frames from the ring
// Vide toutes les frames complétées de l'anneau
func (r *lockFreeCompletionRing) DrainAll() []uint64 {
	completions := make([]uint64, 0, 64)

	for {
		readIdx := atomic.LoadUint64(&r.readIndex)
		writeIdx := atomic.LoadUint64(&r.writeIndex)

		if readIdx >= writeIdx {
			break
		}

		// Batch process multiple items if available (up to 32 at once)
		// Traite en batch si possible (jusqu'à 32 à la fois)
		batchSize := writeIdx - readIdx
		if batchSize > 32 {
			batchSize = 32
		}

		// Try to claim the batch / tente de récupérer le batch
		if atomic.CompareAndSwapUint64(&r.readIndex, readIdx, readIdx+batchSize) {
			for i := uint64(0); i < batchSize; i++ {
				frameAddr := r.ring[(readIdx+i)&4095]
				completions = append(completions, frameAddr)
			}
		}
	}

	return completions
}
