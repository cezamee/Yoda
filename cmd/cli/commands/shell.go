package cli

import (
	"encoding/json"
	"fmt"
	"log"
	"os"

	"github.com/gorilla/websocket"
	"golang.org/x/term"
)

// Message structure for WebSocket communication
type WSMessage struct {
	Type string `json:"type"`
	Data []byte `json:"data,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Cols int    `json:"cols,omitempty"`
}

// runShellSession starts an interactive shell session using WebSocket streaming.
func RunShellSession(conn *websocket.Conn) {
	fmt.Println("🔗 Connected to shell!")

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Printf("Failed to set raw mode: %v", err)
		return
	}
	defer func() {
		term.Restore(int(os.Stdin.Fd()), oldState)
		fmt.Print("\033[2J\033[H")

		// Send close frame to properly close the WebSocket connection
		closeMsg := websocket.FormatCloseMessage(websocket.CloseNormalClosure, "Shell session ended")
		conn.WriteMessage(websocket.CloseMessage, closeMsg)
		fmt.Println("👋 Shell session ended cleanly")
	}()

	// Send terminal size
	if width, height, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		fmt.Printf("📐 Terminal size: %dx%d\n", width, height)
		resizeMsg := WSMessage{
			Type: "resize",
			Rows: height,
			Cols: width,
		}
		msgBytes, _ := json.Marshal(resizeMsg)
		conn.WriteMessage(websocket.TextMessage, msgBytes)
	}

	fmt.Println("✅ Connected! Type 'exit' or press Ctrl+D to return to CLI")

	done := make(chan bool, 1)
	inputDone := make(chan bool, 1)

	// stdin -> WebSocket
	go func() {
		defer func() { inputDone <- true }()
		buf := make([]byte, 1024)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			if n > 0 {
				// Check for Ctrl+D (EOF)
				if n == 1 && buf[0] == 4 {
					done <- true
					return
				}
				msg := WSMessage{
					Type: "data",
					Data: buf[:n],
				}
				msgBytes, err := json.Marshal(msg)
				if err != nil {
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						fmt.Printf("\n📡 Shell WebSocket connection lost unexpectedly: %v\n", err)
					}
					return
				}
			}
		}
	}()

	// WebSocket -> stdout
	go func() {
		defer func() { done <- true }()
		for {
			msgType, msgBytes, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					fmt.Printf("\n📡 Shell WebSocket closed normally: %v\n", err)
				} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					fmt.Printf("\n📡 Shell WebSocket unexpected close: %v\n", err)
				}
				return
			}

			if msgType == websocket.CloseMessage {
				fmt.Printf("\n📡 Received close message from server\n")
				return
			}

			var msg WSMessage
			if err := json.Unmarshal(msgBytes, &msg); err != nil {
				continue
			}
			if msg.Type == "data" && len(msg.Data) > 0 {
				os.Stdout.Write(msg.Data)
			}
		}
	}()

	// Wait for session to end
	select {
	case <-done:
	case <-inputDone:
	}
}
