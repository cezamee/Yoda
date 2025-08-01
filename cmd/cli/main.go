// Minimal Yoda client: connect via TLS and relay stdin/stdout (TTY mode)
package main

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/cezamee/Yoda/internal/core/pb"
	"golang.org/x/term"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

//go:embed certs/client.crt
var clientCertPEM []byte

//go:embed certs/client.key
var clientKeyPEM []byte

//go:embed certs/ca.crt
var caCertPEM []byte

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <server_addr:port>\n", os.Args[0])
		os.Exit(1)
	}
	addr := os.Args[1]

	cert, err := tls.X509KeyPair(clientCertPEM, clientKeyPEM)
	if err != nil {
		log.Fatalf("Failed to load client cert/key: %v", err)
	}
	caPool := x509.NewCertPool()
	if !caPool.AppendCertsFromPEM(caCertPEM) {
		log.Fatalf("Failed to load CA cert")
	}
	creds := credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{cert},
		RootCAs:      caPool,
		MinVersion:   tls.VersionTLS12,
		NextProtos:   []string{"h2"},
	})

	grpcConn, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(creds),
	)
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %v", err)
	}
	defer grpcConn.Close()

	client := pb.NewPTYShellClient(grpcConn)
	stream, err := client.Shell(context.Background())
	if err != nil {
		log.Fatalf("Failed to open PTY shell stream: %v", err)
	}

	if width, height, err := term.GetSize(int(os.Stdin.Fd())); err == nil {
		resizeMsg := fmt.Sprintf("\x1b[8;%d;%dt", height, width)
		stream.Send(&pb.ShellData{Data: []byte(resizeMsg)})
	}

	// Set terminal in raw mode
	oldState, err := term.MakeRaw(int(os.Stdin.Fd()))
	if err != nil {
		log.Fatalf("Failed to set raw mode: %v", err)
	}
	defer term.Restore(int(os.Stdin.Fd()), oldState)

	// Handle Ctrl+C cleanly
	c := make(chan os.Signal, 1)
	signal.Notify(c, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-c
		term.Restore(int(os.Stdin.Fd()), oldState)
		os.Exit(0)
	}()

	// stdin -> gRPC
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if n > 0 {
				stream.Send(&pb.ShellData{Data: buf[:n]})
			}
			if err != nil {
				break
			}
		}
		stream.CloseSend()
	}()

	// gRPC -> stdout
	for {
		resp, err := stream.Recv()
		if err != nil {
			break
		}
		if resp != nil && len(resp.Data) > 0 {
			os.Stdout.Write(resp.Data)
		}
	}
}
