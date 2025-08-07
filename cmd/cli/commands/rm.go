// Rm command implementation for the CLI client
package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// RmMessage structure for WebSocket communication (matches server)
type RmMessage struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
	Removed int    `json:"removed,omitempty"`
}

// rmCommand handles the rm command execution
func RmCommand(conn *websocket.Conn, args []string, recursive bool, force bool) {
	if len(args) == 0 {
		fmt.Printf("❌ Error: rm: missing file operand\n")
		return
	}

	command := "rm"
	if recursive {
		command += " -r"
	}
	if force {
		command += " -f"
	}
	command += " " + strings.Join(args, " ")

	request := RmMessage{
		Type:    "rm",
		Command: command,
	}

	requestBytes, err := json.Marshal(request)
	if err != nil {
		fmt.Printf("❌ Failed to marshal request: %v\n", err)
		return
	}
	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))

	if err := conn.WriteMessage(websocket.TextMessage, requestBytes); err != nil {
		fmt.Printf("❌ Failed to send request: %v\n", err)
		return
	}

	conn.SetReadDeadline(time.Now().Add(60 * time.Second))

	_, responseBytes, err := conn.ReadMessage()
	if err != nil {
		if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
			fmt.Printf("❌ WebSocket connection lost unexpectedly: %v\n", err)
		} else {
			fmt.Printf("❌ Failed to read response: %v\n", err)
		}
		return
	}

	var response RmMessage
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		fmt.Printf("❌ Failed to unmarshal response: %v\n", err)
		return
	}

	// Handle response
	switch response.Type {
	case "rm_result":
		fmt.Printf("🗑️ Command: %s\n", response.Command)
		fmt.Println("=" + strings.Repeat("=", 80))

		lines := strings.Split(response.Output, "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				fmt.Println(line)
			}
		}

		fmt.Println("=" + strings.Repeat("=", 80))

		if response.Removed > 0 {
			fmt.Printf("✅ Successfully removed %d file(s)\n", response.Removed)
		} else {
			fmt.Printf("ℹ️ No files were removed\n")
		}
	case "error":
		fmt.Printf("❌ Error: %s\n", response.Error)
	default:
		fmt.Printf("❌ Unknown response type: %s\n", response.Type)
	}
	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}
