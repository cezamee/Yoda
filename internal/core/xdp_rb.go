// AF_XDP packet processing
package core

import (
	"context"
	"fmt"
	"math/bits"
	"runtime"
	"sync"
	"time"

	cfg "github.com/cezamee/Yoda/internal/config"
	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

var (
	fallbackDestMAC     = []byte{0x52, 0x54, 0x00, 0x12, 0x34, 0x56}
	etherTypeIPv4       = []byte{0x08, 0x00}
	prebuiltEtherHeader = make([]byte, cfg.EthHeaderSize)
	uint64SlicePool     = sync.Pool{
		New: func() any {
			s := make([]uint64, 0, 64)
			return &s
		},
	}
)

func init() {
	copy(prebuiltEtherHeader[0:6], fallbackDestMAC)
	copy(prebuiltEtherHeader[12:14], etherTypeIPv4)
}

func NewRxRingBuffer(size int) *cfg.RxRingBuffer {
	if size&(size-1) != 0 {
		size = 1 << (32 - bits.LeadingZeros32(uint32(size-1)))
	}
	return &cfg.RxRingBuffer{
		Buf:  make([]cfg.RxPacket, size),
		Size: size,
		Mask: size - 1,
	}
}

func NewTxRingBuffer(size int) *cfg.TxRingBuffer {
	if size&(size-1) != 0 {
		size = 1 << (32 - bits.LeadingZeros32(uint32(size-1)))
	}
	return &cfg.TxRingBuffer{
		Buf:  make([][]byte, size),
		Size: size,
		Mask: size - 1,
	}
}

func PushRxPacket(r *cfg.RxRingBuffer, val cfg.RxPacket) bool {
	if r.Count == r.Size {
		return false
	}
	r.Buf[r.Tail] = val
	r.Tail = (r.Tail + 1) & r.Mask
	r.Count++
	return true
}

func PopRxPacket(r *cfg.RxRingBuffer) (cfg.RxPacket, bool) {
	if r.Count == 0 {
		return cfg.RxPacket{}, false
	}
	val := r.Buf[r.Head]
	r.Head = (r.Head + 1) & r.Mask
	r.Count--
	return val, true
}

func PushTxPacket(r *cfg.TxRingBuffer, val []byte) bool {
	if r.Count == r.Size {
		return false
	}
	r.Buf[r.Tail] = val
	r.Tail = (r.Tail + 1) & r.Mask
	r.Count++
	return true
}

func PopTxPacket(r *cfg.TxRingBuffer) ([]byte, bool) {
	if r.Count == 0 {
		return nil, false
	}
	val := r.Buf[r.Head]
	r.Head = (r.Head + 1) & r.Mask
	r.Count--
	return val, true
}

// Start main AF_XDP packet processing loop
func StartPacketProcessing(b *cfg.NetstackBridge) {

	if b.RxRing == nil {
		b.RxRing = NewRxRingBuffer(4096)
	}
	if b.TxRing == nil {
		b.TxRing = NewTxRingBuffer(4096)
	}

	b.Cb.UMEM.Lock()
	b.Cb.Fill.FillAll(&b.Cb.UMEM)
	b.Cb.UMEM.Unlock()

	go func() {
		handleOutboundPackets(b)
	}()

	statsTicker := time.NewTicker(50 * time.Second)
	defer statsTicker.Stop()

	sleepDuration := 100 * time.Nanosecond
	maxSleep := 10 * time.Microsecond
	minSleep := 100 * time.Nanosecond

	for {
		workDone := false

		select {
		case <-statsTicker.C:
			printStats(b)
		default:
			// STEP 1: Process TX completion queue FIRST
			if processCompletionQueue(b) {
				workDone = true
			}

			// STEP 2: Process incoming RX packets
			if processRXQueue(b) {
				workDone = true
			}

			// STEP 3: Maintain Fill queue
			maintainFillQueue(b)

			if workDone {
				sleepDuration = minSleep
			} else {
				if sleepDuration < maxSleep {
					sleepDuration = sleepDuration * 2
					if sleepDuration > maxSleep {
						sleepDuration = maxSleep
					}
				}
			}

			if sleepDuration > 1*time.Microsecond {
				time.Sleep(sleepDuration)
			} else {
				runtime.Gosched()
			}
		}
	}
}

// Process TX completion queue
func processCompletionQueue(b *cfg.NetstackBridge) bool {
	b.Cb.UMEM.Lock()
	nCompleted, completionIndex := b.Cb.Completion.Peek()
	if nCompleted > 0 {
		completedFramesPtr := uint64SlicePool.Get().(*[]uint64)
		completedFrames := *completedFramesPtr
		completedFrames = completedFrames[:0]

		if cap(completedFrames) < int(nCompleted) {
			completedFrames = make([]uint64, nCompleted)
		} else {
			completedFrames = completedFrames[:nCompleted]
		}

		for i := uint32(0); i < nCompleted; i++ {
			completedFrames[i] = b.Cb.Completion.Get(completionIndex + i)
		}
		b.Cb.Completion.Release(nCompleted)
		for _, frameAddr := range completedFrames {
			b.Cb.UMEM.FreeFrame(frameAddr)
		}

		*completedFramesPtr = completedFrames
		uint64SlicePool.Put(completedFramesPtr)

		b.Cb.UMEM.Unlock()
		return true
	}
	b.Cb.UMEM.Unlock()
	return false
}

// Process RX queue with batch processing
func processRXQueue(b *cfg.NetstackBridge) bool {
	b.Cb.UMEM.Lock()
	nReceived, index := b.Cb.RX.Peek()
	if nReceived == 0 {
		b.Cb.UMEM.Unlock()
		return false
	}

	// Store packets in RX ring buffer first
	for i := uint32(0); i < nReceived; i++ {
		desc := b.Cb.RX.Get(index + i)
		packetData := b.Cb.UMEM.Get(desc)
		pkt := cfg.RxPacket{
			Buffer:    packetData,
			FrameAddr: uint64(desc.Addr),
		}
		PushRxPacket(b.RxRing, pkt)
	}

	b.Cb.RX.Release(nReceived)
	b.Cb.UMEM.Unlock()

	// Process packets from ring buffer WITHOUT holding UMEM lock
	framesToFreePtr := uint64SlicePool.Get().(*[]uint64)
	framesToFree := *framesToFreePtr
	framesToFree = framesToFree[:0]

	for {
		pkt, ok := PopRxPacket(b.RxRing)
		if !ok {
			break
		}
		processPacket(b, pkt.Buffer)
		framesToFree = append(framesToFree, pkt.FrameAddr)
	}

	// Batch free RX frames
	b.Cb.UMEM.Lock()
	for _, frameAddr := range framesToFree {
		b.Cb.UMEM.FreeFrame(frameAddr)
	}
	b.Cb.UMEM.Unlock()

	*framesToFreePtr = framesToFree
	uint64SlicePool.Put(framesToFreePtr)

	return true
}

func maintainFillQueue(b *cfg.NetstackBridge) {
	b.Cb.UMEM.Lock()
	b.Cb.Fill.FillAll(&b.Cb.UMEM)
	b.Cb.UMEM.Unlock()
}

func handleOutboundPackets(b *cfg.NetstackBridge) {

	ctx := context.Background()
	for {
		pkt := b.LinkEP.ReadContext(ctx)
		if pkt == nil {
			fmt.Printf("📡 ReadContext returned nil, checking termination...\n")
			continue
		}
		data := pkt.ToView().AsSlice()
		sendPacketTX(b, data)
		pkt.DecRef()
	}
}

func sendPacketTX(b *cfg.NetstackBridge, ipData []byte) {
	if len(ipData) < cfg.IpHeaderMinSize {
		return
	}

	if !PushTxPacket(b.TxRing, ipData) {
		return
	}

	b.Cb.UMEM.Lock()
	defer b.Cb.UMEM.Unlock()

	// FIRST: Process completion queue to free up sent frames
	nCompleted, completionIndex := b.Cb.Completion.Peek()
	if nCompleted > 0 {
		completedFramesPtr := uint64SlicePool.Get().(*[]uint64)
		completedFrames := *completedFramesPtr
		completedFrames = completedFrames[:0]

		if cap(completedFrames) < int(nCompleted) {
			completedFrames = make([]uint64, nCompleted)
		} else {
			completedFrames = completedFrames[:nCompleted]
		}

		for i := uint32(0); i < nCompleted; i++ {
			completedFrames[i] = b.Cb.Completion.Get(completionIndex + i)
		}
		b.Cb.Completion.Release(nCompleted)

		for _, frameAddr := range completedFrames {
			b.Cb.UMEM.FreeFrame(frameAddr)
		}

		*completedFramesPtr = completedFrames
		uint64SlicePool.Put(completedFramesPtr)
	}

	// Process multiple TX packets in one lock cycle
	packetsProcessed := 0
	maxBatch := 16 // Process up to 16 packets per lock cycle

	for packetsProcessed < maxBatch {
		data, ok := PopTxPacket(b.TxRing)
		if !ok {
			break
		}

		// SECOND: Try to reserve a TX descriptor
		nReserved, index := b.Cb.TX.Reserve(&b.Cb.UMEM, 1)
		if nReserved == 0 {
			PushTxPacket(b.TxRing, data)
			break
		}

		// THIRD: Get free frame for packet
		frameAddr := b.Cb.UMEM.AllocFrame()
		if frameAddr == 0 {
			PushTxPacket(b.TxRing, data)
			break
		}

		frame := b.Cb.UMEM.Get(unix.XDPDesc{Addr: frameAddr, Len: uint32(cfg.EthHeaderSize + len(data))})
		if len(frame) < cfg.EthHeaderSize+len(data) || cfg.EthHeaderSize+len(data) > cfg.FrameSize {
			b.Cb.UMEM.FreeFrame(frameAddr)
			PushTxPacket(b.TxRing, data)
			break
		}

		copy(frame[0:cfg.EthHeaderSize], prebuiltEtherHeader)
		if b.ClientMAC != [6]byte{} {
			copy(frame[0:6], b.ClientMAC[:])
		}

		copy(frame[6:12], b.SrcMAC)
		copy(frame[cfg.EthHeaderSize:], data)

		desc := unix.XDPDesc{Addr: frameAddr, Len: uint32(cfg.EthHeaderSize + len(data))}
		b.Cb.TX.Set(index, desc)
		b.Cb.TX.Notify()

		packetsProcessed++
	}
}

func processPacket(b *cfg.NetstackBridge, packetData []byte) {
	if len(packetData) < (cfg.EthHeaderSize + cfg.IpHeaderMinSize) {
		return
	}

	if b.ClientMAC == [6]byte{} {
		copy(b.ClientMAC[:], packetData[6:12])
	}

	ipPacket := packetData[cfg.EthHeaderSize:]

	pkt := stack.NewPacketBuffer(stack.PacketBufferOptions{
		Payload: buffer.MakeWithData(ipPacket),
	})
	b.LinkEP.InjectInbound(ipv4.ProtocolNumber, pkt)
	pkt.DecRef()
}
