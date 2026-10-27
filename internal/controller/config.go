package controller

import (
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/slipramp/slipramp/internal/auth"
	"github.com/slipramp/slipramp/internal/cluster"
	"github.com/slipramp/slipramp/internal/kubevalidate"
	"github.com/slipramp/slipramp/internal/metricsserver"
	"github.com/slipramp/slipramp/internal/promotion"
)

const DefaultMetricsAddress = metricsserver.DefaultAddress

type Config struct {
	ClusterName         string
	ServerURL           string
	MetricsAddress      string
	EnrollmentToken     string
	BearerToken         string
	CheckoutImage       string
	HarnessImage        string
	GitCredentials      map[string]string
	ExecutionNamespaces []string
	KargoIntegrations   []promotion.ObserverConfig
}

func (c Config) Validate() error {
	if err := cluster.ValidateName(c.ClusterName); err != nil {
		return err
	}

	serverURL, err := url.Parse(c.ServerURL)
	if err != nil {
		return fmt.Errorf("parse server URL: %w", err)
	}

	if (serverURL.Scheme != "http" && serverURL.Scheme != "https") ||
		serverURL.Host == "" {
		return errors.New("server URL must be an absolute HTTP or HTTPS URL")
	}

	if serverURL.User != nil {
		return errors.New("server URL must not contain user information")
	}

	if serverURL.RawQuery != "" || serverURL.Fragment != "" {
		return errors.New("server URL must not contain a query or fragment")
	}

	if err := metricsserver.ValidateAddress(c.MetricsAddress); err != nil {
		return err
	}

	if err := auth.ValidateBearerToken(c.BearerToken); err != nil {
		return fmt.Errorf("validate controller credential: %w", err)
	}

	if err := auth.ValidateBearerToken(c.EnrollmentToken); err != nil {
		return fmt.Errorf("validate controller enrollment credential: %w", err)
	}

	if err := validateImage("checkout image", c.CheckoutImage); err != nil {
		return err
	}

	if err := validateImage("harness image", c.HarnessImage); err != nil {
		return err
	}

	if len(c.ExecutionNamespaces) == 0 {
		return errors.New("at least one execution namespace is required")
	}
	seenNamespaces := make(map[string]struct{}, len(c.ExecutionNamespaces))
	for _, namespace := range c.ExecutionNamespaces {
		if err := kubevalidate.DNSLabel("execution namespace", namespace); err != nil {
			return err
		}
		if _, ok := seenNamespaces[namespace]; ok {
			return fmt.Errorf("execution namespace %q is configured more than once", namespace)
		}
		seenNamespaces[namespace] = struct{}{}
	}

	for _, name := range slices.Sorted(maps.Keys(c.GitCredentials)) {
		if err := kubevalidate.DNSLabel("Git credential name", name); err != nil {
			return err
		}

		if err := kubevalidate.DNSSubdomain(
			fmt.Sprintf("Git credential %q secret name", name),
			c.GitCredentials[name],
		); err != nil {
			return err
		}
	}

	if err := promotion.ValidateObserverConfigs(c.KargoIntegrations); err != nil {
		return fmt.Errorf("validate Kargo observers: %w", err)
	}

	return nil
}

func (c Config) OwnsExecutionNamespace(namespace string) bool {
	return slices.Contains(c.ExecutionNamespaces, namespace)
}

func validateImage(subject, image string) error {
	if strings.TrimSpace(image) == "" {
		return fmt.Errorf("%s is required", subject)
	}

	if strings.ContainsAny(image, "\x00\t\r\n ") {
		return fmt.Errorf("%s must not contain whitespace", subject)
	}

	return nil
}
