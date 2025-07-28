// tools/gen_certs.go
package main

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"
)

func main() {
	// Check internal/certs for server
	if _, err := os.Stat("internal/core/certs"); os.IsNotExist(err) {
		fmt.Println("❌ The folder internal/core/certs does not exist. Please create it before running this script.")
		os.Exit(1)
	}
	// Check cmd/cli/certs for client
	if _, err := os.Stat("cmd/cli/certs"); os.IsNotExist(err) {
		fmt.Println("❌ The folder cmd/cli/certs does not exist. Please create it before running this script.")
		os.Exit(1)
	}

	// 1. CA
	caKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	randOrg := make([]byte, 8)
	rand.Read(randOrg)
	orgStr := fmt.Sprintf("YodaCA-%X", randOrg)
	caTemplate := &x509.Certificate{
		SerialNumber:          big.NewInt(2025),
		Subject:               pkix.Name{Organization: []string{orgStr}},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().AddDate(10, 0, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	caCertDER, _ := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	writePem("internal/core/certs/ca.crt", "CERTIFICATE", caCertDER)
	writePem("cmd/cli/certs/ca.crt", "CERTIFICATE", caCertDER)
	writePem("internal/core/certs/ca.key", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(caKey))

	// 2. Server
	serverKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	randCNServ := make([]byte, 8)
	rand.Read(randCNServ)
	cnStrServ := fmt.Sprintf("YodaServer-%X", randCNServ)
	serverIP := "127.0.0.1"
	if len(os.Args) > 1 {
		serverIP = os.Args[1]
	}
	serverTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2026),
		Subject:      pkix.Name{CommonName: cnStrServ},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(5, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP(serverIP)},
	}
	serverCertDER, _ := x509.CreateCertificate(rand.Reader, serverTemplate, caTemplate, &serverKey.PublicKey, caKey)
	writePem("internal/core/certs/server.crt", "CERTIFICATE", serverCertDER)
	writePem("internal/core/certs/server.key", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(serverKey))

	// 3. Client
	clientKey, _ := rsa.GenerateKey(rand.Reader, 2048)
	randCNCli := make([]byte, 8)
	rand.Read(randCNCli)
	cnStrCli := fmt.Sprintf("YodaClient-%X", randCNCli)
	clientTemplate := &x509.Certificate{
		SerialNumber: big.NewInt(2027),
		Subject:      pkix.Name{CommonName: cnStrCli},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().AddDate(5, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	clientCertDER, _ := x509.CreateCertificate(rand.Reader, clientTemplate, caTemplate, &clientKey.PublicKey, caKey)
	writePem("cmd/cli/certs/client.crt", "CERTIFICATE", clientCertDER)
	writePem("cmd/cli/certs/client.key", "RSA PRIVATE KEY", x509.MarshalPKCS1PrivateKey(clientKey))

	fmt.Println("✅ Certificates generated")
}

func writePem(path, typ string, der []byte) {
	f, _ := os.Create(path)
	defer f.Close()
	pem.Encode(f, &pem.Block{Type: typ, Bytes: der})
}
