package tlsconf

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

type testCA struct {
	cert *x509.Certificate
	key  *rsa.PrivateKey
	file string // PEM path
}

func newTestCA(t *testing.T, dir string) *testCA {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "gogate-test-ca"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "ca.pem")
	writePEM(t, file, "CERTIFICATE", der)
	return &testCA{cert: cert, key: key, file: file}
}

// leaf signs a leaf cert for cn and returns (certFile, keyFile).
func (ca *testCA) leaf(t *testing.T, dir, cn string) (string, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
	}
	if ip := net.ParseIP(cn); ip != nil {
		tmpl.IPAddresses = []net.IP{ip}
	} else {
		tmpl.DNSNames = []string{cn}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.cert, &key.PublicKey, ca.key)
	if err != nil {
		t.Fatal(err)
	}
	certFile := filepath.Join(dir, cn+".pem")
	keyFile := filepath.Join(dir, cn+"-key.pem")
	writePEM(t, certFile, "CERTIFICATE", der)
	keyDER, _ := x509.MarshalPKCS8PrivateKey(key)
	writePEM(t, keyFile, "PRIVATE KEY", keyDER)
	return certFile, keyFile
}

func writePEM(t *testing.T, path, typ string, der []byte) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := pem.Encode(f, &pem.Block{Type: typ, Bytes: der}); err != nil {
		t.Fatal(err)
	}
}

func TestBaseHardening(t *testing.T) {
	c := Base()
	if c.MinVersion != tls.VersionTLS12 {
		t.Fatalf("MinVersion = %x", c.MinVersion)
	}
	if len(c.CipherSuites) == 0 {
		t.Fatal("no cipher suites pinned for TLS 1.2")
	}
}

// TestMutualTLSEndToEnd: a listener that requires a client cert accepts a client
// presenting one from the shared CA; a client with no cert is rejected; and the
// pinned-CA upstream config verifies the server.
func TestMutualTLSEndToEnd(t *testing.T) {
	dir := t.TempDir()
	ca := newTestCA(t, dir)
	srvCert, srvKey := ca.leaf(t, dir, "127.0.0.1")
	cliCert, cliKey := ca.leaf(t, dir, "gogate-client")

	srvCfg, err := Server(srvCert, srvKey, ca.file)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := tls.Listen("tcp", "127.0.0.1:0", srvCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("mtls-ok"))
	})}
	go srv.Serve(ln)
	defer srv.Close()
	url := "https://" + ln.Addr().String() + "/"

	upCfg, err := Upstream(ca.file, cliCert, cliKey)
	if err != nil {
		t.Fatal(err)
	}
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: upCfg}}
	resp, err := client.Get(url)
	if err != nil {
		t.Fatalf("mTLS request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status %d", resp.StatusCode)
	}

	// A client with no cert must be rejected by RequireAndVerifyClientCert.
	bareCfg, _ := Upstream(ca.file, "", "")
	bare := &http.Client{Transport: &http.Transport{TLSClientConfig: bareCfg}}
	if _, err := bare.Get(url); err == nil {
		t.Fatal("server should reject a client with no certificate")
	}
}
