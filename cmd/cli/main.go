// Minimal Yoda client: connect via TLS and relay stdin/stdout (TTY mode)
package main

import (
	"crypto/tls"
	"crypto/x509"
	_ "embed"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/cezamee/Yoda/internal/core/pb"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
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
		grpc.WithReadBufferSize(64*1024),
		grpc.WithWriteBufferSize(64*1024),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:                30 * time.Second,
			Timeout:             5 * time.Second,
			PermitWithoutStream: true,
		}),
	)
	if err != nil {
		log.Fatalf("Failed to connect to gRPC server: %v", err)
	}
	defer grpcConn.Close()

	client := pb.NewPTYShellClient(grpcConn)

	if err := RunCLI(client); err != nil {
		log.Fatalf("CLI error: %v", err)
	}
}
