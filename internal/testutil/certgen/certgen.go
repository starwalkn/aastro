// Package certgen provides in-process certificate generation and TLS probes for tests.
package certgen

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"io"
	"math/big"
	"net"
	"os"
	"time"
)

func must(err error) {
	if err != nil {
		panic(err)
	}
}

func must1[T any](v T, err error) T {
	must(err)
	return v
}

func NewCA() (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	key := must1(ecdsa.GenerateKey(elliptic.P256(), rand.Reader))

	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}

	der := must1(x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key))
	cert := must1(x509.ParseCertificate(der))
	caPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	return cert, key, caPEM
}

func NewLeaf(ca *x509.Certificate, caKey *ecdsa.PrivateKey, serial int64) (certPEM, keyPEM []byte) {
	key := must1(ecdsa.GenerateKey(elliptic.P256(), rand.Reader))

	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(serial),
		Subject:      pkix.Name{CommonName: "leaf"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		DNSNames:     []string{"localhost"},
	}

	der := must1(x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey))
	keyDER := must1(x509.MarshalECPrivateKey(key))

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
}

func KeyPair(certPEM, keyPEM []byte) tls.Certificate {
	return must1(tls.X509KeyPair(certPEM, keyPEM))
}

func WriteAtomic(path string, data []byte) {
	tmp := path + ".tmp"

	must(os.WriteFile(tmp, data, 0o600))
	must(os.Rename(tmp, path))
}

func ServedSerial(cfg *tls.Config) (int64, error) {
	ln, err := tls.Listen("tcp", "127.0.0.1:0", cfg)
	if err != nil {
		return 0, err
	}
	defer ln.Close()

	go func() {
		conn, accErr := ln.Accept()
		if accErr != nil {
			return
		}
		defer conn.Close()

		_ = conn.(*tls.Conn).Handshake()
	}()

	conn, err := tls.Dial("tcp", ln.Addr().String(), &tls.Config{
		InsecureSkipVerify: true,
		ServerName:         "localhost",
	})
	if err != nil {
		return 0, err
	}
	defer conn.Close()

	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return 0, errors.New("no peer certificates")
	}

	return certs[0].SerialNumber.Int64(), nil
}

func StartTLSServer(cfg *tls.Config) (string, func()) {
	ln := must1(tls.Listen("tcp", "127.0.0.1:0", cfg))

	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}

			go func(c net.Conn) {
				defer c.Close()
				_ = c.(*tls.Conn).Handshake()
			}(conn)
		}
	}()

	return ln.Addr().String(), func() { _ = ln.Close() }
}

func MTLSDial(addr string, clientCert tls.Certificate) error {
	conn, err := tls.Dial("tcp", addr, &tls.Config{
		Certificates:       []tls.Certificate{clientCert},
		InsecureSkipVerify: true,
		ServerName:         "localhost",
	})
	if err != nil {
		return err
	}
	defer conn.Close()

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))

	if _, readErr := conn.Read(make([]byte, 1)); readErr != nil && !errors.Is(readErr, io.EOF) {
		return readErr
	}

	return nil
}
