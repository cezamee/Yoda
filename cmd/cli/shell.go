package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/cezamee/Yoda/internal/core/pb"
	"golang.org/x/term"
)

// runShellSession starts an interactive shell session using gRPC streaming.
func runShellSession(client pb.PTYShellClient) {
	fmt.Println("🔗 Connecting to shell...")

	stream, err := client.Shell(context.Background())
	if err != nil {
		log.Printf("Shell connection failed: %v", err)
		return
	}
	defer stream.CloseSend()

	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Printf("Failed to set raw mode: %v", err)
		return
	}
	defer func() {
		term.Restore(int(os.Stdin.Fd()), oldState)
		fmt.Print("\033[2J\033[H")
		fmt.Println("� Shell session ended - returning to CLI")
	}()

	if width, height, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		fmt.Printf("📐 Terminal size: %dx%d\n", width, height)
		resizeMsg := fmt.Sprintf("\x1b[8;%d;%dt", height, width)
		stream.Send(&pb.ShellData{Data: []byte(resizeMsg)})
	}

	fmt.Println("✅ Connected! Type 'exit' or press Ctrl+D to return to CLI")

	done := make(chan bool, 1)
	inputDone := make(chan bool, 1)

	// stdin -> gRPC
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
				if err := stream.Send(&pb.ShellData{Data: buf[:n]}); err != nil {
					return
				}
			}
		}
	}()

	// gRPC -> stdout
	go func() {
		defer func() { done <- true }()
		for {
			resp, err := stream.Recv()
			if err != nil {
				return
			}
			if resp != nil && len(resp.Data) > 0 {
				os.Stdout.Write(resp.Data)
			}
		}
	}()

	// Wait for session to end
	select {
	case <-done:
	case <-inputDone:
	}
}
