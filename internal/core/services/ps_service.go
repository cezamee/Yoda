// Native Go process listing service: provides ps and pstree functionality over WebSocket
// Service de listage des processus natif Go : fournit les fonctionnalités ps et pstree via WebSocket
package services

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/gorilla/websocket"
	"github.com/shirou/gopsutil/v3/process"
)

// PSMessage structure for WebSocket communication
type PSMessage struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// ProcessInfo represents process information
type ProcessInfo struct {
	PID     int    `json:"pid"`
	PPID    int    `json:"ppid"`
	Command string `json:"command"`
	State   string `json:"state"`
	User    string `json:"user"`
	CPU     string `json:"cpu"`
	Memory  string `json:"memory"`
	Start   string `json:"start"`
	TTY     string `json:"tty"`
}

// HandleWebSocketPSSession handles ps commands over WebSocket
func HandleWebSocketPSSession(conn *websocket.Conn) {
	fmt.Printf("🔍 Starting PS service session\n")

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("🚨 PS service panic: %v\n", r)
		}
		fmt.Printf("🧹 Cleaning up PS service session...\n")
		conn.Close()
	}()

	for {
		msgType, msgBytes, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
				fmt.Printf("📡 WebSocket closed normally: %v\n", err)
			} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				fmt.Printf("📡 WebSocket unexpected close: %v\n", err)
			} else {
				fmt.Printf("📡 WebSocket closed: %v\n", err)
			}
			return
		}

		// Handle close messages
		if msgType == websocket.CloseMessage {
			fmt.Printf("📡 Received close message from client\n")
			return
		}

		var msg PSMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			sendPSError(conn, "Invalid JSON message")
			continue
		}

		switch msg.Type {
		case "ps":
			handleNativePSCommand(conn, msg.Command)
		default:
			sendPSError(conn, "Unknown message type: "+msg.Type)
		}
	}
}

// handleNativePSCommand processes ps commands
func handleNativePSCommand(conn *websocket.Conn, command string) {
	var output string
	var cmdStr string

	processes, err := getProcessList()
	if err != nil {
		sendPSError(conn, fmt.Sprintf("Failed to get process list: %v", err))
		return
	}

	if strings.Contains(command, "-t") || strings.Contains(command, "tree") {
		output = generateProcessTree(processes)
		cmdStr = "ps tree"
	} else {
		output = generatePSAuxOutput(processes)
		cmdStr = "ps aux"
	}

	fmt.Printf("🔍 Executing: %s\n", cmdStr)

	response := PSMessage{
		Type:    "ps_result",
		Command: cmdStr,
		Output:  output,
	}

	msgBytes, err := json.Marshal(response)
	if err != nil {
		sendPSError(conn, "Failed to marshal response")
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			fmt.Printf("❌ WebSocket unexpected close during send: %v\n", err)
		} else {
			fmt.Printf("❌ Failed to send response: %v\n", err)
		}
		return
	}

	fmt.Printf("✅ PS command executed successfully\n")
}

// getProcessList get all process information
func getProcessList() ([]ProcessInfo, error) {
	var processes []ProcessInfo

	pids, err := process.Pids()
	if err != nil {
		return nil, err
	}

	for _, pid := range pids {
		proc, err := process.NewProcess(pid)
		if err != nil {
			continue
		}

		processInfo, err := getProcessInfo(proc)
		if err != nil {
			continue
		}

		processes = append(processes, processInfo)
	}

	return processes, nil
}

// getProcessInfo extracts process information
func getProcessInfo(proc *process.Process) (ProcessInfo, error) {
	var info ProcessInfo

	info.PID = int(proc.Pid)

	if ppid, err := proc.Ppid(); err == nil {
		info.PPID = int(ppid)
	}

	// Get command
	if cmdline, err := proc.Cmdline(); err == nil && cmdline != "" {
		info.Command = cmdline
	} else if name, err := proc.Name(); err == nil {
		info.Command = fmt.Sprintf("[%s]", name)
	} else {
		info.Command = "[unknown]"
	}

	// Get status/state
	if status, err := proc.Status(); err == nil && len(status) > 0 {
		info.State = status[0]
	} else {
		info.State = "?"
	}

	// Get username
	if username, err := proc.Username(); err == nil {
		info.User = username
	} else {
		info.User = "unknown"
	}

	// Get CPU percentage
	if cpuPercent, err := proc.CPUPercent(); err == nil {
		info.CPU = fmt.Sprintf("%.1f", cpuPercent)
	} else {
		info.CPU = "0.0"
	}

	// Get memory info (RSS in KB)
	if memInfo, err := proc.MemoryInfo(); err == nil {
		info.Memory = fmt.Sprintf("%d", memInfo.RSS/1024)
	} else {
		info.Memory = "0"
	}

	// TTY info - simplified
	info.TTY = "?"

	// Start time - simplified
	info.Start = "00:00"

	return info, nil
}

// truncateString truncates a string to a maximum length
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// generatePSAuxOutput creates ps aux style output
func generatePSAuxOutput(processes []ProcessInfo) string {
	var output strings.Builder

	output.WriteString(fmt.Sprintf("%-12s %6s %8s %4s %s\n",
		"USER", "PID", "MEM(KB)", "%CPU", "COMMAND"))

	// Sort by PID
	sort.Slice(processes, func(i, j int) bool {
		return processes[i].PID < processes[j].PID
	})

	for _, proc := range processes {
		memory := strings.TrimSuffix(proc.Memory, " kB")
		if memory == "0" || memory == "" {
			memory = "-"
		}

		command := proc.Command
		if len(command) > 80 {
			command = truncateString(command, 80)
		}

		line := fmt.Sprintf("%-12s %6d %8s %4s %s\n",
			truncateString(proc.User, 12),
			proc.PID,
			memory,
			proc.CPU,
			command,
		)
		output.WriteString(line)
	}

	return output.String()
}

// generateProcessTree creates a process tree output
func generateProcessTree(processes []ProcessInfo) string {
	var output strings.Builder

	output.WriteString("Process Tree (PID and Command)\n")
	output.WriteString(strings.Repeat("=", 50) + "\n")

	childMap := make(map[int][]ProcessInfo)
	processMap := make(map[int]ProcessInfo)

	for _, proc := range processes {
		processMap[proc.PID] = proc
		childMap[proc.PPID] = append(childMap[proc.PPID], proc)
	}

	// Sort children by PID
	for ppid := range childMap {
		sort.Slice(childMap[ppid], func(i, j int) bool {
			return childMap[ppid][i].PID < childMap[ppid][j].PID
		})
	}

	visited := make(map[int]bool)

	var rootPIDs []int
	for _, proc := range processes {
		if proc.PPID == 0 || processMap[proc.PPID].PID == 0 {
			rootPIDs = append(rootPIDs, proc.PID)
		}
	}

	sort.Ints(rootPIDs)

	for _, rootPID := range rootPIDs {
		if !visited[rootPID] {
			printProcessTree(&output, processMap, childMap, rootPID, "", visited)
		}
	}

	return output.String()
}

// printProcessTree recursively prints the process tree
func printProcessTree(output *strings.Builder, processMap map[int]ProcessInfo, childMap map[int][]ProcessInfo, pid int, prefix string, visited map[int]bool) {
	if visited[pid] {
		return
	}
	visited[pid] = true

	proc, exists := processMap[pid]
	if !exists {
		return
	}

	command := proc.Command
	if len(command) > 50 {
		command = truncateString(command, 50)
	}
	output.WriteString(fmt.Sprintf("%s%s (%d) [%s]\n", prefix, command, proc.PID, proc.State))

	var basePrefix string
	if strings.HasSuffix(prefix, "├── ") {
		basePrefix = strings.TrimSuffix(prefix, "├── ") + "│   "
	} else if strings.HasSuffix(prefix, "└── ") {
		basePrefix = strings.TrimSuffix(prefix, "└── ") + "    "
	} else {
		basePrefix = prefix
	}

	children := childMap[pid]
	for i, child := range children {
		isLast := i == len(children)-1

		var childPrefix string
		if isLast {
			childPrefix = basePrefix + "└── "
		} else {
			childPrefix = basePrefix + "├── "
		}
		printProcessTree(output, processMap, childMap, child.PID, childPrefix, visited)
	}
}

// sendPSError sends an error message to the client
func sendPSError(conn *websocket.Conn, errorMsg string) {
	response := PSMessage{
		Type:  "error",
		Error: errorMsg,
	}

	msgBytes, err := json.Marshal(response)
	if err != nil {
		fmt.Printf("❌ Failed to marshal error response: %v\n", err)
		return
	}

	if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			fmt.Printf("❌ WebSocket unexpected close during error send: %v\n", err)
		} else {
			fmt.Printf("❌ Failed to send error response: %v\n", err)
		}
	}
}
