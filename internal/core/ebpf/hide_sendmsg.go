package ebpf

import (
	"bytes"
	_ "embed"
	"fmt"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

//go:embed obj/hook_sendmsg.o
var hookSendMsgObj []byte

func LoadAndAttachHookSendMsg() (link.Link, error) {
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(hookSendMsgObj))
	if err != nil {
		return nil, fmt.Errorf("failed to load spec: %w", err)
	}

	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to create collection: %w", err)
	}

	prog := coll.Programs["kprobe_sendmsg"]
	if prog == nil {
		return nil, fmt.Errorf("kprobe_sendmsg program not found")
	}

	kp, err := link.Kprobe("__x64_sys_sendmsg", prog, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to attach kprobe: %w", err)
	}

	fmt.Printf("👻 sendmsg kprobe loaded and attached\n")
	return kp, nil
}
