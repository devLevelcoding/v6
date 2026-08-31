// Package tlsconf builds hardened *tls.Config values for GoGate's listener and
// for its upstream connections (CoverGo P4). Defaults: TLS 1.2 minimum, a
// modern cipher suite list for the 1.2 handshakes Go doesn't hard-code, and
// optional mutual TLS in both directions.
package tlsconf

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
)

// hardened1_2Suites are the AEAD suites we allow for TLS 1.2 (1.3 suites are
// not configurable in Go and are always safe).
var hardened1_2Suites = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
	tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
}

// Base returns a *tls.Config with the shared hardening applied.
func Base() *tls.Config {
	return &tls.Config{
		MinVersion:   tls.VersionTLS12,
		CipherSuites: hardened1_2Suites,
	}
}

// Server builds the listener config. certFile/keyFile are required. If
// clientCAFile is set, clients must present a certificate signed by that CA
// (inbound mTLS).
func Server(certFile, keyFile, clientCAFile string) (*tls.Config, error) {
	if certFile == "" || keyFile == "" {
		return nil, errors.New("tlsconf: server needs a cert and key")
	}
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("tlsconf: load keypair: %w", err)
	}
	cfg := Base()
	cfg.Certificates = []tls.Certificate{cert}
	if clientCAFile != "" {
		pool, err := loadCAs(clientCAFile)
		if err != nil {
			return nil, err
		}
		cfg.ClientCAs = pool
		cfg.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return cfg, nil
}

// Upstream builds the config the proxy uses when dialing an upstream over TLS.
// caFile pins the roots the upstream cert must chain to (empty = system roots).
// certFile/keyFile, if set, present a client certificate (outbound mTLS).
func Upstream(caFile, certFile, keyFile string) (*tls.Config, error) {
	cfg := Base()
	if caFile != "" {
		pool, err := loadCAs(caFile)
		if err != nil {
			return nil, err
		}
		cfg.RootCAs = pool
	}
	if certFile != "" || keyFile != "" {
		if certFile == "" || keyFile == "" {
			return nil, errors.New("tlsconf: upstream mTLS needs both a cert and a key")
		}
		cert, err := tls.LoadX509KeyPair(certFile, keyFile)
		if err != nil {
			return nil, fmt.Errorf("tlsconf: load client keypair: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}
	return cfg, nil
}

func loadCAs(file string) (*x509.CertPool, error) {
	pem, err := os.ReadFile(file)
	if err != nil {
		return nil, fmt.Errorf("tlsconf: read CA %q: %w", file, err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return nil, fmt.Errorf("tlsconf: %q has no PEM certificates", file)
	}
	return pool, nil
}
