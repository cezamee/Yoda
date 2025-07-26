/*
TLS and Netstack integration for Yoda AF_XDP server
Intégration TLS et Netstack pour le serveur Yoda AF_XDP

- Manages TLS certificate generation and secure connections
- Provides thread-safe TLS writes to prevent MAC corruption
- Bridges XDP, netstack, and eBPF components for high-performance networking

- Gère la génération de certificats TLS et les connexions sécurisées
- Fournit des écritures TLS thread-safe pour éviter la corruption MAC
- Relie XDP, netstack et eBPF pour le réseau haute performance
*/
package core

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/cilium/ebpf"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/xdp"
)

// NetstackBridge links XDP, netstack, and TLS components
// NetstackBridge relie les composants XDP, netstack et TLS
type NetstackBridge struct {
	Cb        *xdp.ControlBlock // XDP control block / Bloc de contrôle XDP
	QueueID   uint32            // XDP queue ID / Identifiant de file XDP
	Stack     *stack.Stack      // Gvisor netstack / Netstack Gvisor
	LinkEP    *channel.Endpoint // Netstack endpoint / Point de terminaison netstack
	StatsMap  *ebpf.Map         // eBPF stats map / Map eBPF statistiques
	ClientMAC [6]byte           // Fixed-size MAC array / Tableau MAC taille fixe
	SrcMAC    []byte            // Source MAC address / Adresse MAC source
	TlsMutex  sync.Mutex        // TLS write protection / Protection écriture TLS
}

func generateSelfSignedCert() (tls.Certificate, error) {
	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return tls.Certificate{}, err
	}

	// Certificate template / Modèle de certificat
	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization:  []string{"Yoda Corp"},
			Country:       []string{"US"},
			Province:      []string{""},
			Locality:      []string{"San Francisco"},
			StreetAddress: []string{""},
			PostalCode:    []string{""},
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	template.IPAddresses = append(template.IPAddresses, net.ParseIP(netLocalIP))
	template.IPAddresses = append(template.IPAddresses, net.ParseIP("127.0.0.1"))
	template.DNSNames = append(template.DNSNames, "localhost")
	template.DNSNames = append(template.DNSNames, "Yoda")
	template.DNSNames = append(template.DNSNames, netLocalIP)

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		return tls.Certificate{}, err
	}

	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(priv)})

	return tls.X509KeyPair(certPEM, keyPEM)
}

// safeTLSWrite writes to TLS connection with mutex and delay to prevent MAC corruption
// safeTLSWrite écrit sur la connexion TLS avec mutex et délai pour éviter la corruption MAC
func (b *NetstackBridge) safeTLSWrite(tlsConn *tls.Conn, data []byte) error {
	b.TlsMutex.Lock()
	defer b.TlsMutex.Unlock()

	_, err := tlsConn.Write(data)
	if err != nil {
		return err
	}

	// Small delay to prevent TLS MAC corruption
	// Petit délai pour éviter la corruption MAC
	// TODO: Any other way to handle this?
	time.Sleep(60 * time.Microsecond)
	return nil
}
