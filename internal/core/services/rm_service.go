// Native Go file removal service: provides rm functionality with wildcard support over WebSocket
package services

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
)

type RmMessage struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
	Removed int    `json:"removed,omitempty"`
}

func HandleWebSocketRmSession(conn *websocket.Conn) {
	fmt.Printf("🗑️ Starting Rm service session\n")

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("🚨 Rm service panic: %v\n", r)
		}
		fmt.Printf("🧹 Cleaning up Rm service session...\n")
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

		var msg RmMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			sendRmError(conn, "Invalid JSON message")
			continue
		}

		switch msg.Type {
		case "rm":
			handleRmCommand(conn, msg.Command)
		default:
			sendRmError(conn, "Unknown message type: "+msg.Type)
		}
	}
}

func handleRmCommand(conn *websocket.Conn, command string) {
	args := strings.Fields(command)
	var paths []string
	var recursive bool
	var force bool

	if len(args) <= 1 {
		sendRmError(conn, "rm: missing file operand")
		return
	}

	for i := 1; i < len(args); i++ {
		arg := args[i]
		if strings.HasPrefix(arg, "-") {
			if strings.Contains(arg, "r") || strings.Contains(arg, "R") {
				recursive = true
			}
			if strings.Contains(arg, "f") {
				force = true
			}
		} else {
			paths = append(paths, arg)
		}
	}

	if len(paths) == 0 {
		sendRmError(conn, "rm: missing file operand")
		return
	}

	var output strings.Builder
	totalRemoved := 0
	removedFiles := []string{}

	for _, path := range paths {
		matches, err := filepath.Glob(path)
		if err != nil {
			sendRmError(conn, fmt.Sprintf("Invalid pattern '%s': %v", path, err))
			return
		}

		if len(matches) == 0 {
			if hasWildcards([]string{path}) {
				if !force {
					sendRmError(conn, fmt.Sprintf("rm: cannot remove '%s': No such file or directory", path))
					return
				}
				continue
			} else {
				if !force {
					if _, err := os.Stat(path); err != nil {
						sendRmError(conn, fmt.Sprintf("rm: cannot remove '%s': No such file or directory", path))
						return
					}
				}
				matches = []string{path}
			}
		}

		for _, match := range matches {
			if err := removeFile(match, recursive, force); err != nil {
				if !force {
					sendRmError(conn, fmt.Sprintf("rm: cannot remove '%s': %v", match, err))
					return
				}
				continue
			}

			removedFiles = append(removedFiles, match)
			totalRemoved++
		}
	}

	if totalRemoved > 0 {
		if len(paths) > 1 || hasWildcards(paths) {
			output.WriteString(fmt.Sprintf("\033[1;33mRemoved %d file(s) matching patterns:\033[0m\n", totalRemoved))
		} else {
			output.WriteString(fmt.Sprintf("Removed %d file(s):\n", totalRemoved))
		}
		for _, file := range removedFiles {
			output.WriteString(fmt.Sprintf("\033[1;31m- %s\033[0m\n", file))
		}
	} else {
		output.WriteString("No files removed\n")
	}

	fmt.Printf("🗑️ Executing: rm command - removed %d files\n", totalRemoved)

	response := RmMessage{
		Type:    "rm_result",
		Command: command,
		Output:  output.String(),
		Removed: totalRemoved,
	}

	msgBytes, err := json.Marshal(response)
	if err != nil {
		sendRmError(conn, "Failed to marshal response")
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

	fmt.Printf("✅ Rm command executed successfully\n")
}

func removeFile(filePath string, recursive bool, force bool) error {
	stat, err := os.Stat(filePath)
	if err != nil {
		if force && os.IsNotExist(err) {
			return nil
		}
		return err
	}

	if stat.IsDir() {
		if !recursive {
			return fmt.Errorf("is a directory (use -r to remove directories)")
		}
		return os.RemoveAll(filePath)
	}

	return os.Remove(filePath)
}

func sendRmError(conn *websocket.Conn, errorMsg string) {
	response := RmMessage{
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
