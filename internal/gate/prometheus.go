package gate

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/auth"
)

const (
	MaximumPrometheusResponseSize = 1 << 20
	maximumCredentialFileSize     = 1 << 20
)

type ObservationState string

const (
	ObservationPassing     ObservationState = "passing"
	ObservationBlocking    ObservationState = "blocking"
	ObservationUnavailable ObservationState = "unavailable"
)

// Observation contains only the normalized value and a bounded diagnostic.
// Prometheus labels and response bodies never leave the evaluator.
type Observation struct {
	State      ObservationState
	Value      *float64
	Diagnostic string
}

type PrometheusEvaluator struct {
	endpoint    *url.URL
	httpClient  *http.Client
	bearerToken string
}

func NewPrometheusEvaluator(source PrometheusSource) (*PrometheusEvaluator, error) {
	if err := source.validate(); err != nil {
		return nil, err
	}

	transport, bearerToken, err := prometheusTransport(source)
	if err != nil {
		return nil, err
	}
	baseURL, err := url.Parse(source.URL)
	if err != nil {
		return nil, errors.New("parse validated Prometheus URL")
	}

	return &PrometheusEvaluator{
		endpoint:    baseURL.JoinPath("api/v1/query"),
		httpClient:  &http.Client{Transport: transport, Timeout: source.Timeout},
		bearerToken: bearerToken,
	}, nil
}

func (e *PrometheusEvaluator) Evaluate(
	ctx context.Context,
	definition Definition,
	evaluationTime time.Time,
) Observation {
	if err := definition.Validate(); err != nil {
		return unavailable("Gate definition is invalid.")
	}

	requestURL := *e.endpoint
	parameters := requestURL.Query()
	parameters.Set("query", definition.Prometheus.Query)
	parameters.Set("time", evaluationTime.UTC().Format(time.RFC3339Nano))
	requestURL.RawQuery = parameters.Encode()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL.String(), nil)
	if err != nil {
		return unavailable("Prometheus request could not be created.")
	}
	request.Header.Set("Accept", "application/json")
	if e.bearerToken != "" {
		request.Header.Set("Authorization", "Bearer "+e.bearerToken)
	}

	response, err := e.httpClient.Do(request)
	if err != nil {
		return unavailable(prometheusRequestDiagnostic(ctx, err))
	}
	defer func() {
		_ = response.Body.Close()
	}()

	contents, oversized, err := readBounded(response.Body, MaximumPrometheusResponseSize)
	if err != nil {
		return unavailable("Prometheus response could not be read.")
	}
	if oversized {
		return unavailable("Prometheus response exceeded the size limit.")
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return unavailable("Prometheus source returned HTTP " + strconv.Itoa(response.StatusCode) + ".")
	}

	value, diagnostic := prometheusValue(contents)
	if diagnostic != "" {
		return unavailable(diagnostic)
	}
	passing, err := definition.Prometheus.Condition.Compare(value)
	if err != nil {
		return unavailable("Prometheus result could not be compared.")
	}

	state := ObservationBlocking
	if passing {
		state = ObservationPassing
	}

	return Observation{State: state, Value: &value}
}

func prometheusTransport(source PrometheusSource) (*http.Transport, string, error) {
	transport := http.DefaultTransport.(*http.Transport).Clone()

	bearerToken := ""
	if source.BearerTokenFile != "" {
		contents, err := readConfigurationFile(source.BearerTokenFile)
		if err != nil {
			return nil, "", fmt.Errorf("read Prometheus bearer token file: %w", err)
		}
		bearerToken = strings.TrimSpace(string(contents))
		if err := auth.ValidateBearerToken(bearerToken); err != nil {
			return nil, "", errors.New( //nolint:staticcheck // Prometheus is a product name.
				"Prometheus bearer token file contains an invalid token",
			)
		}
	}

	if source.TLS == (TLSConfig{}) {
		return transport, bearerToken, nil
	}

	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if source.TLS.CAFile != "" {
		contents, err := readConfigurationFile(source.TLS.CAFile)
		if err != nil {
			return nil, "", fmt.Errorf("read Prometheus TLS CA file: %w", err)
		}
		rootCAs, err := x509.SystemCertPool()
		if err != nil {
			return nil, "", errors.New("load system certificate pool for Prometheus")
		}
		if !rootCAs.AppendCertsFromPEM(contents) {
			return nil, "", errors.New( //nolint:staticcheck // Prometheus is a product name.
				"Prometheus TLS CA file does not contain a certificate",
			)
		}
		tlsConfig.RootCAs = rootCAs
	}
	if source.TLS.ClientCertificateFile != "" {
		certificatePEM, err := readConfigurationFile(source.TLS.ClientCertificateFile)
		if err != nil {
			return nil, "", fmt.Errorf("read Prometheus TLS client certificate file: %w", err)
		}
		keyPEM, err := readConfigurationFile(source.TLS.ClientKeyFile)
		if err != nil {
			return nil, "", fmt.Errorf("read Prometheus TLS client key file: %w", err)
		}
		certificate, err := tls.X509KeyPair(certificatePEM, keyPEM)
		if err != nil {
			return nil, "", errors.New( //nolint:staticcheck // Prometheus is a product name.
				"Prometheus TLS client certificate or key is invalid",
			)
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

func readBounded(reader io.Reader, maximum int64) ([]byte, bool, error) {
	contents, err := io.ReadAll(io.LimitReader(reader, maximum+1))
	if err != nil {
		return nil, false, err
	}
	if int64(len(contents)) > maximum {
		return nil, true, nil
	}

	return contents, false, nil
}

func prometheusRequestDiagnostic(ctx context.Context, err error) string {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "Prometheus request was canceled."
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "Prometheus request timed out."
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		return "Prometheus request timed out."
	}

	return "Prometheus request failed before a response was received."
}

func prometheusValue(contents []byte) (float64, string) {
	var envelope struct {
		Status string `json:"status"`
		Data   struct {
			ResultType string          `json:"resultType"`
			Result     json.RawMessage `json:"result"`
		} `json:"data"`
	}
	if err := json.Unmarshal(contents, &envelope); err != nil {
		return 0, "Prometheus response was malformed."
	}
	if envelope.Status != "success" {
		return 0, "Prometheus query was unsuccessful."
	}

	switch envelope.Data.ResultType {
	case "scalar":
		value, err := prometheusSampleValue(envelope.Data.Result)
		if err != nil {
			return 0, "Prometheus scalar result was invalid."
		}

		return value, ""
	case "vector":
		var samples []struct {
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(envelope.Data.Result, &samples); err != nil {
			return 0, "Prometheus vector result was malformed."
		}
		if len(samples) != 1 {
			return 0, "Prometheus vector result must contain exactly one sample."
		}
		value, err := prometheusSampleValue(samples[0].Value)
		if err != nil {
			return 0, "Prometheus vector sample was invalid."
		}

		return value, ""
	default:
		return 0, "Prometheus result type is unsupported."
	}
}

func prometheusSampleValue(raw json.RawMessage) (float64, error) {
	var sample []json.RawMessage
	if err := json.Unmarshal(raw, &sample); err != nil || len(sample) != 2 {
		return 0, errors.New("invalid sample")
	}

	var timestamp float64
	if err := json.Unmarshal(sample[0], &timestamp); err != nil || !isFinite(timestamp) {
		return 0, errors.New("invalid sample timestamp")
	}
	var encodedValue string
	if err := json.Unmarshal(sample[1], &encodedValue); err != nil {
		return 0, errors.New("invalid sample value")
	}
	value, err := strconv.ParseFloat(encodedValue, 64)
	if err != nil || !isFinite(value) {
		return 0, errors.New("invalid sample value")
	}

	return value, nil
}

func unavailable(diagnostic string) Observation {
	return Observation{State: ObservationUnavailable, Diagnostic: diagnostic}
}
