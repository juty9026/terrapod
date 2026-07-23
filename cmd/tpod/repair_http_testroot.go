//go:build tpod_repair_testroot

package main

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"os"
	"time"
)

var repairTestCAFile string

func repairHTTPClient() *http.Client {
	certificate, err := os.ReadFile(repairTestCAFile)
	if err != nil {
		panic(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificate) {
		panic("invalid repair test CA")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{
		MinVersion: tls.VersionTLS12,
		RootCAs:    roots,
	}
	return &http.Client{Timeout: 30 * time.Second, Transport: transport}
}
