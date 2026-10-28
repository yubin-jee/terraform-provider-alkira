// Command sandbox is a local, in-memory stand-in for the Alkira REST API covering
// the endpoints terraform-provider-alkira uses for Lumen Multi-Cloud Gateways,
// their connectors, segments, groups and billing tags. It is not the Alkira portal.
package main

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"flag"
	"log"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

func main() {
	apiAddr := flag.String("api-addr", "127.0.0.1:8443", "HTTPS address for the Alkira API (the provider's portal)")
	uiAddr := flag.String("ui-addr", ":8090", "HTTP address for the read-only Lumen Connect page")
	dataDir := flag.String("data-dir", "sandbox/.data", "directory for the generated TLS certificate")
	delay := flag.Duration("provision-delay", 3*time.Second, "how long a new gateway stays PROVISIONING")
	seedExternal := flag.Bool("seed-external", false, "start with one external gateway (created in Lumen Connect, read-only here)")
	flag.Parse()

	certFile, keyFile, err := ensureCert(*dataDir)
	if err != nil {
		log.Fatalf("certificate: %v", err)
	}
	store := NewStore(*delay, *seedExternal)

	go func() {
		log.Printf("Lumen Connect sandbox page on http://localhost%s", portOf(*uiAddr))
		log.Fatal(http.ListenAndServe(*uiAddr, store.UIHandler()))
	}()
	log.Printf("Alkira API sandbox on https://%s (trust %s via SSL_CERT_FILE)", *apiAddr, certFile)
	log.Fatal(http.ListenAndServeTLS(*apiAddr, certFile, keyFile, store.APIHandler()))
}

func portOf(addr string) string {
	if _, p, err := net.SplitHostPort(addr); err == nil {
		return ":" + p
	}
	return addr
}

// ensureCert creates (once) a self-signed certificate for localhost; it doubles as
// its own CA so it can be passed to Terraform as SSL_CERT_FILE.
func ensureCert(dir string) (string, string, error) {
	certFile, keyFile := filepath.Join(dir, "sandbox-cert.pem"), filepath.Join(dir, "sandbox-key.pem")
	if _, err := tls.LoadX509KeyPair(certFile, keyFile); err == nil {
		return certFile, keyFile, nil
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", "", err
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return "", "", err
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: "lumen-connect-sandbox", Organization: []string{"local sandbox"}},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
		IsCA:                  true,
		DNSNames:              []string{"localhost"},
		IPAddresses:           []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return "", "", err
	}
	keyDER, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		return "", "", err
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER}), 0o600); err != nil {
		return "", "", err
	}
	return certFile, keyFile, nil
}
