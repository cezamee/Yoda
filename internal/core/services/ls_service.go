// Native Go file listing service: provides ls functionality with wildcard support over WebSocket
package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/gorilla/websocket"
)

type LSMessage struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

type FileInfo struct {
	Name        string      `json:"name"`
	Size        int64       `json:"size"`
	Mode        os.FileMode `json:"mode"`
	ModTime     time.Time   `json:"modtime"`
	IsDir       bool        `json:"isdir"`
	Permissions string      `json:"permissions"`
	Owner       string      `json:"owner"`
	Group       string      `json:"group"`
	Links       uint64      `json:"links"`
}

func HandleWebSocketLSSession(conn *websocket.Conn) {
	fmt.Printf("📁 Starting LS service session\n")

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("🚨 LS service panic: %v\n", r)
		}
		fmt.Printf("🧹 Cleaning up LS service session...\n")
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

		if msgType == websocket.CloseMessage {
			fmt.Printf("📡 Received close message from client\n")
			return
		}

		var msg LSMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			sendLSError(conn, "Invalid JSON message")
			continue
		}

		switch msg.Type {
		case "ls":
			handleLSCommand(conn, msg.Command)
		default:
			sendLSError(conn, "Unknown message type: "+msg.Type)
		}
	}
}

func handleLSCommand(conn *websocket.Conn, command string) {
	args := strings.Fields(command)
	var paths []string

	if len(args) <= 1 {
		paths = []string{"."}
	} else {
		paths = args[1:]
	}

	dirFiles := make(map[string][]FileInfo)
	var output strings.Builder

	for _, path := range paths {
		matches, err := filepath.Glob(path)
		if err != nil {
			sendLSError(conn, fmt.Sprintf("Invalid pattern '%s': %v", path, err))
			return
		}

		if len(matches) == 0 {
			if _, err := os.Stat(path); err != nil {
				sendLSError(conn, fmt.Sprintf("ls: cannot access '%s': No such file or directory", path))
				return
			}
			matches = []string{path}
		}

		for _, match := range matches {
			files, err := getFileList(match)
			if err != nil {
				sendLSError(conn, fmt.Sprintf("Failed to list '%s': %v", match, err))
				return
			}
			parentDir := filepath.Dir(match)
			if len(files) == 1 && !files[0].IsDir {
				dirFiles[parentDir] = append(dirFiles[parentDir], files...)
			} else {
				dirFiles[match] = append(dirFiles[match], files...)
			}
		}
	}
	output.WriteString(generateStructuredLSOutput(dirFiles, len(paths) > 1 || hasWildcards(paths)))

	fmt.Printf("📁 Executing: ls command with %d directories\n", len(dirFiles))

	response := LSMessage{
		Type:    "ls_result",
		Command: "ls -al",
		Output:  output.String(),
	}

	msgBytes, err := json.Marshal(response)
	if err != nil {
		sendLSError(conn, "Failed to marshal response")
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

	fmt.Printf("✅ LS command executed successfully\n")
}

func getFileList(path string) ([]FileInfo, error) {
	var files []FileInfo

	stat, err := os.Stat(path)
	if err != nil {
		return nil, err
	}

	if stat.IsDir() {
		entries, err := os.ReadDir(path)
		if err != nil {
			return nil, err
		}

		dotInfo, err := getFileInfo(path, ".")
		if err == nil {
			files = append(files, dotInfo)
		}

		parentPath := filepath.Dir(path)
		if parentPath != path {
			dotDotInfo, err := getFileInfo(parentPath, "..")
			if err == nil {
				files = append(files, dotDotInfo)
			}
		}

		for _, entry := range entries {
			entryPath := filepath.Join(path, entry.Name())
			fileInfo, err := getFileInfo(entryPath, entry.Name())
			if err != nil {
				continue
			}
			files = append(files, fileInfo)
		}
	} else {
		fileInfo, err := getFileInfo(path, filepath.Base(path))
		if err != nil {
			return nil, err
		}
		files = append(files, fileInfo)
	}

	return files, nil
}

func getFileInfo(fullPath, displayName string) (FileInfo, error) {
	var info FileInfo

	stat, err := os.Stat(fullPath)
	if err != nil {
		return info, err
	}

	info.Name = displayName
	info.Size = stat.Size()
	info.Mode = stat.Mode()
	info.ModTime = stat.ModTime()
	info.IsDir = stat.IsDir()
	info.Permissions = stat.Mode().String()

	if sysstat, ok := stat.Sys().(*syscall.Stat_t); ok {
		info.Links = sysstat.Nlink
		info.Owner = getUserName(sysstat.Uid)
		info.Group = getGroupName(sysstat.Gid)
	} else {
		info.Links = 1
		info.Owner = "unknown"
		info.Group = "unknown"
	}

	return info, nil
}

func getUserName(uid uint32) string {
	passwdData, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return fmt.Sprintf("%d", uid)
	}

	lines := strings.Split(string(passwdData), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) >= 3 {
			if uidStr := fields[2]; uidStr == fmt.Sprintf("%d", uid) {
				return fields[0]
			}
		}
	}
	return fmt.Sprintf("%d", uid)
}

func getGroupName(gid uint32) string {
	groupData, err := os.ReadFile("/etc/group")
	if err != nil {
		return fmt.Sprintf("%d", gid)
	}

	lines := strings.Split(string(groupData), "\n")
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) >= 3 {
			if gidStr := fields[2]; gidStr == fmt.Sprintf("%d", gid) {
				return fields[0]
			}
		}
	}
	return fmt.Sprintf("%d", gid)
}

func generateStructuredLSOutput(dirFiles map[string][]FileInfo, multipleTargets bool) string {
	var output strings.Builder

	var dirs []string
	for dir := range dirFiles {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)

	for i, dir := range dirs {
		files := dirFiles[dir]

		if multipleTargets && len(dirs) > 1 {
			if i > 0 {
				output.WriteString("\n")
			}
			output.WriteString(fmt.Sprintf("%s:\n", dir))
		}
		output.WriteString(generateLSOutput(files))
	}

	return output.String()
}

func generateLSOutput(files []FileInfo) string {
	var output strings.Builder

	sort.Slice(files, func(i, j int) bool {
		if files[i].Name == "." {
			return true
		}
		if files[j].Name == "." {
			return false
		}
		if files[i].Name == ".." {
			return true
		}
		if files[j].Name == ".." {
			return false
		}

		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})

	totalBlocks := 0
	for _, file := range files {
		blocks := (file.Size + 1023) / 1024
		totalBlocks += int(blocks)
	}
	if len(files) > 0 && files[0].Name != "." {
		output.WriteString(fmt.Sprintf("total %d\n", totalBlocks))
	}

	for _, file := range files {
		timeStr := file.ModTime.Format("Jan 02 15:04")
		if time.Since(file.ModTime) > 365*24*time.Hour {
			timeStr = file.ModTime.Format("Jan 02  2006")
		}

		sizeStr := fmt.Sprintf("%8d", file.Size)
		if file.IsDir {
			sizeStr = fmt.Sprintf("%8s", "4096")
		}

		line := fmt.Sprintf("%s %3d %-8s %-8s %s %s %s\n",
			file.Permissions,
			file.Links,
			truncateField(file.Owner, 8),
			truncateField(file.Group, 8),
			sizeStr,
			timeStr,
			file.Name,
		)
		output.WriteString(line)
	}

	return output.String()
}

func truncateField(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen-1] + "+"
}

func sendLSError(conn *websocket.Conn, errorMsg string) {
	response := LSMessage{
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
