// Native Go file reading service: provides cat functionality with wildcard support over WebSocket
package services

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/gorilla/websocket"
)

// CatMessage structure for WebSocket communication
type CatMessage struct {
	Type     string `json:"type"`
	Command  string `json:"command,omitempty"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// HandleWebSocketCatSession handles cat commands over WebSocket
func HandleWebSocketCatSession(conn *websocket.Conn) {
	fmt.Printf("📄 Starting Cat service session\n")

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("🚨 Cat service panic: %v\n", r)
		}
		fmt.Printf("🧹 Cleaning up Cat service session...\n")
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

		var msg CatMessage
		if err := json.Unmarshal(msgBytes, &msg); err != nil {
			sendCatError(conn, "Invalid JSON message")
			continue
		}

		switch msg.Type {
		case "cat":
			handleCatCommand(conn, msg.Command)
		default:
			sendCatError(conn, "Unknown message type: "+msg.Type)
		}
	}
}

// handleCatCommand processes cat commands with wildcard support
func handleCatCommand(conn *websocket.Conn, command string) {
	args := strings.Fields(command)
	var paths []string

	if len(args) <= 1 {
		sendCatError(conn, "cat: missing file operand")
		return
	} else {
		paths = args[1:]
	}

	var output strings.Builder
	totalFiles := 0

	for _, path := range paths {
		matches, err := filepath.Glob(path)
		if err != nil {
			sendCatError(conn, fmt.Sprintf("Invalid pattern '%s': %v", path, err))
			return
		}

		if len(matches) == 0 {
			if _, err := os.Stat(path); err != nil {
				sendCatError(conn, fmt.Sprintf("cat: %s: No such file or directory", path))
				return
			}
			matches = []string{path}
		}

		for _, match := range matches {
			content, filename, err := readFileContent(match)
			if err != nil {
				sendCatError(conn, fmt.Sprintf("cat: %s: %v", match, err))
				return
			}

			if len(paths) > 1 || hasWildcards(paths) {
				if totalFiles > 0 {
					output.WriteString("\n")
				}
				if len(matches) > 1 || len(paths) > 1 {
					output.WriteString(fmt.Sprintf("\n\033[1;36m==> %s <==\033[0m\n", filename))
				}
			}

			output.WriteString(content)
			totalFiles++
		}
	}

	fmt.Printf("📄 Executing: cat command with %d files\n", totalFiles)

	response := CatMessage{
		Type:    "cat_result",
		Command: command,
		Output:  output.String(),
	}

	msgBytes, err := json.Marshal(response)
	if err != nil {
		sendCatError(conn, "Failed to marshal response")
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

	fmt.Printf("✅ Cat command executed successfully\n")
}

// readFileContent reads the content of a file and returns it along with the filename
func readFileContent(filePath string) (string, string, error) {
	stat, err := os.Stat(filePath)
	if err != nil {
		return "", "", err
	}

	if stat.IsDir() {
		return "", "", fmt.Errorf("is a directory")
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", "", err
	}
	defer file.Close()

	content, err := io.ReadAll(file)
	if err != nil {
		return "", "", err
	}

	return string(content), filepath.Base(filePath), nil
}

// sendCatError sends an error message to the client
func sendCatError(conn *websocket.Conn, errorMsg string) {
	response := CatMessage{
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
