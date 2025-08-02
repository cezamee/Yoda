// AF_XDP packet processing and optimization utilities
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

// RxPacket structure for received packets
type RxPacket struct {
	buffer    []byte
	frameAddr uint64
}

type RxRingBuffer struct {
	buf   []RxPacket
	size  int
	head  int
	tail  int
	count int
	mask  int
}

type TxRingBuffer struct {
	buf   [][]byte
	size  int
	head  int
	tail  int
	count int
	mask  int
}

func NewRxRingBuffer(size int) *RxRingBuffer {
	if size&(size-1) != 0 {
		size = 1 << (32 - bits.LeadingZeros32(uint32(size-1)))
	}
	return &RxRingBuffer{
		buf:  make([]RxPacket, size),
		size: size,
		mask: size - 1,
	}
}

func NewTxRingBuffer(size int) *TxRingBuffer {
	if size&(size-1) != 0 {
		size = 1 << (32 - bits.LeadingZeros32(uint32(size-1)))
	}
	return &TxRingBuffer{
		buf:  make([][]byte, size),
		size: size,
		mask: size - 1,
	}
}

func (r *RxRingBuffer) Push(val RxPacket) bool {
	if r.count == r.size {
		return false
	}
	r.buf[r.tail] = val
	r.tail = (r.tail + 1) & r.mask
	r.count++
	return true
}

func (r *RxRingBuffer) Pop() (RxPacket, bool) {
	if r.count == 0 {
		return RxPacket{}, false
	}
	val := r.buf[r.head]
	r.head = (r.head + 1) & r.mask
	r.count--
	return val, true
}

func (r *TxRingBuffer) Push(val []byte) bool {
	if r.count == r.size {
		return false
	}
	r.buf[r.tail] = val
	r.tail = (r.tail + 1) & r.mask
	r.count++
	return true
}

func (r *TxRingBuffer) Pop() ([]byte, bool) {
	if r.count == 0 {
		return nil, false
	}
	val := r.buf[r.head]
	r.head = (r.head + 1) & r.mask
	r.count--
	return val, true
}

// Start main AF_XDP packet processing loop
func (b *NetstackBridge) StartPacketProcessing() {

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
		b.handleOutboundPackets()
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
			b.printStats()
		default:
			// STEP 1: Process TX completion queue FIRST
			if b.processCompletionQueue() {
				workDone = true
			}

			// STEP 2: Process incoming RX packets
			if b.processRXQueue() {
				workDone = true
			}

			// STEP 3: Maintain Fill queue
			b.maintainFillQueue()

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
func (b *NetstackBridge) processCompletionQueue() bool {
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
func (b *NetstackBridge) processRXQueue() bool {
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
		pkt := RxPacket{
			buffer:    packetData,
			frameAddr: uint64(desc.Addr),
		}
		b.RxRing.Push(pkt)
	}

	b.Cb.RX.Release(nReceived)
	b.Cb.UMEM.Unlock()

	// Process packets from ring buffer WITHOUT holding UMEM lock
	framesToFreePtr := uint64SlicePool.Get().(*[]uint64)
	framesToFree := *framesToFreePtr
	framesToFree = framesToFree[:0]

	for {
		pkt, ok := b.RxRing.Pop()
		if !ok {
			break
		}
		b.processPacket(pkt.buffer)
		framesToFree = append(framesToFree, pkt.frameAddr)
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

func (b *NetstackBridge) maintainFillQueue() {
	b.Cb.UMEM.Lock()
	b.Cb.Fill.FillAll(&b.Cb.UMEM)
	b.Cb.UMEM.Unlock()
}

func (b *NetstackBridge) handleOutboundPackets() {

	ctx := context.Background()
	for {
		pkt := b.LinkEP.ReadContext(ctx)
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
	if len(ipData) < cfg.IpHeaderMinSize {
		return
	}

	if !b.TxRing.Push(ipData) {
		return
	}

	// Process multiple packets in one lock cycle
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
		data, ok := b.TxRing.Pop()
		if !ok {
			break
		}

		// SECOND: Try to reserve a TX descriptor
		nReserved, index := b.Cb.TX.Reserve(&b.Cb.UMEM, 1)
		if nReserved == 0 {
			b.TxRing.Push(data)
			break
		}

		// THIRD: Get free frame for packet
		frameAddr := b.Cb.UMEM.AllocFrame()
		if frameAddr == 0 {
			b.TxRing.Push(data)
			break
		}

		frame := b.Cb.UMEM.Get(unix.XDPDesc{Addr: frameAddr, Len: uint32(cfg.EthHeaderSize + len(data))})
		if len(frame) < cfg.EthHeaderSize+len(data) || cfg.EthHeaderSize+len(data) > cfg.FrameSize {
			b.Cb.UMEM.FreeFrame(frameAddr)
			b.TxRing.Push(data)
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

func (b *NetstackBridge) processPacket(packetData []byte) {
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
