// LS command implementation for the CLI client
package cli

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// LSMessage structure for WebSocket communication (matches server)
type LSMessage struct {
	Type    string `json:"type"`
	Command string `json:"command,omitempty"`
	Output  string `json:"output,omitempty"`
	Error   string `json:"error,omitempty"`
}

// lsCommand handles the ls command execution
func LsCommand(conn *websocket.Conn, args []string) {
	command := "ls"
	if len(args) > 0 {
		command += " " + strings.Join(args, " ")
	}

	request := LSMessage{
		Type:    "ls",
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

	var response LSMessage
	if err := json.Unmarshal(responseBytes, &response); err != nil {
		fmt.Printf("❌ Failed to unmarshal response: %v\n", err)
		return
	}

	// Handle response
	switch response.Type {
	case "ls_result":
		fmt.Printf("📁 Command: %s\n", response.Command)
		fmt.Println("=" + strings.Repeat("=", 80))

		lines := strings.Split(response.Output, "\n")
		for _, line := range lines {
			if strings.TrimSpace(line) != "" {
				if strings.HasSuffix(line, ":") && !strings.HasPrefix(line, "d") && !strings.HasPrefix(line, "-") {
					fmt.Printf("\n\033[1;33m%s\033[0m\n", line)
				} else if strings.HasPrefix(line, "d") {
					fmt.Printf("\033[1;34m%s\033[0m\n", line)
				} else if strings.Contains(line, "->") {
					fmt.Printf("\033[1;36m%s\033[0m\n", line)
				} else if strings.HasPrefix(line, "-rwx") || strings.HasPrefix(line, "-r-x") {
					fmt.Printf("\033[1;32m%s\033[0m\n", line)
				} else if strings.HasPrefix(line, "total") {
					fmt.Printf("\033[1m%s\033[0m\n", line)
				} else {
					fmt.Println(line)
				}
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
