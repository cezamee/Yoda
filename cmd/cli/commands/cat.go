// Cat command implementation for the CLI client
package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// CatMessage structure for WebSocket communication (matches server)
type CatMessage struct {
	Type     string `json:"type"`
	Command  string `json:"command,omitempty"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	Filename string `json:"filename,omitempty"`
}

// CatCommand handles the cat command execution
func CatCommand(conn *websocket.Conn, args []string) {
	if len(args) == 0 {
		fmt.Printf("❌ Error: cat: missing file operand\n")
		return
	}

	command := "cat " + strings.Join(args, " ")

	request := CatMessage{
		Type:    "cat",
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

	var response CatMessage
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		fmt.Printf("❌ Failed to unmarshal response: %v\n", err)
		return
	}

	// Handle response
	switch response.Type {
	case "cat_result":
		if strings.TrimSpace(response.Output) != "" {
			fmt.Print(response.Output)
			if !strings.HasSuffix(response.Output, "\n") {
				fmt.Println()
			}
		}
	case "error":
		fmt.Printf("❌ Error: %s\n", response.Error)
	default:
		fmt.Printf("❌ Unknown response type: %s\n", response.Type)
	}
	conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""))
}
