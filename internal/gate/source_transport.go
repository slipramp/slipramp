package gate

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/slipramp/slipramp/internal/auth"
)

const maximumCredentialFileSize = 1 << 20

func sourceTransport(source HTTPSource) (*http.Transport, string, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	bearerToken := ""
	if source.BearerTokenFile != "" {
		contents, err := readConfigurationFile(source.BearerTokenFile)
		if err != nil {
			return nil, "", fmt.Errorf("read source bearer token file: %w", err)
		}
		bearerToken = strings.TrimSpace(string(contents))
		if err := auth.ValidateBearerToken(bearerToken); err != nil {
			return nil, "", errors.New("source bearer token file contains an invalid token")
		}
	}

	if source.TLS == (TLSConfig{}) {
		return transport, bearerToken, nil
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if source.TLS.CAFile != "" {
		contents, err := readConfigurationFile(source.TLS.CAFile)
		if err != nil {
			return nil, "", fmt.Errorf("read source TLS CA file: %w", err)
		}
		rootCAs, err := x509.SystemCertPool()
		if err != nil {
			return nil, "", errors.New("load system certificate pool for Gate source")
		}
		if !rootCAs.AppendCertsFromPEM(contents) {
			return nil, "", errors.New("source TLS CA file does not contain a certificate")
		}
		tlsConfig.RootCAs = rootCAs
	}
	if source.TLS.ClientCertificateFile != "" {
		certificatePEM, err := readConfigurationFile(source.TLS.ClientCertificateFile)
		if err != nil {
			return nil, "", fmt.Errorf("read source TLS client certificate file: %w", err)
		}
		keyPEM, err := readConfigurationFile(source.TLS.ClientKeyFile)
		if err != nil {
			return nil, "", fmt.Errorf("read source TLS client key file: %w", err)
		}
		certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
		if err != nil {
			return nil, "", errors.New("source TLS client certificate or key is invalid")
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}
	transport.TLSClientConfig = tlsConfig

	return transport, bearerToken, nil
}

func readConfigurationFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	contents, oversized, err := readBounded(file, maximumCredentialFileSize)
	if err != nil {
		return nil, err
	}
	if oversized {
		return nil, errors.New("file exceeds the size limit")
	}

	return contents, nil
}
