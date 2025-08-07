// PS command implementation for the CLI client
package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// PSMessage structure for WebSocket communication (matches server)
type PSMessage struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// psCommand handles the ps command execution
func PsCommand(conn *websocket.Conn, tree bool) {
	command := "ps"
	if tree {
		command += " -t"
	}

	request := PSMessage{
		Type:    "ps",
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

	var response PSMessage
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		fmt.Printf("❌ Failed to unmarshal response: %v\n", err)
		return
	}

	// Handle response
	switch response.Type {
	case "ps_result":
		fmt.Printf("📋 Command: %s\n", response.Command)
		fmt.Println("=" + strings.Repeat("=", 80))

		lines := strings.Split(response.Output, "\n")
		for i, line := range lines {
			if i == 0 {
				fmt.Printf("\033[1;36m%s\033[0m\n", line)
			} else if strings.TrimSpace(line) != "" {
				fmt.Println(line)
			}
		}

		fmt.Println("=" + strings.Repeat("=", 80))
	case "error":
		fmt.Printf("❌ Error: %s\n", response.Error)
	default:
		fmt.Printf("❌ Unknown response type: %s\n", response.Type)
	}

	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}
