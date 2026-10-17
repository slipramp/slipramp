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
)

type Config struct {
	ClusterName    string
	ServerURL      string
	BearerToken    string
	CheckoutImage  string
	HarnessImage   string
	GitCredentials map[string]string
}

func (c Config) Validate() error {
	if err := cluster.ValidateName(c.ClusterName); err != nil {
		return err
	}

	serverURL, err := url.Parse(c.ServerURL)
	if err != nil {
		return fmt.Errorf("parse server URL: %w", err)
	}

	if serverURL.Scheme != "https" || serverURL.Host == "" {
		return errors.New("server URL must be an absolute HTTPS URL")
	}

	if serverURL.User != nil {
		return errors.New("server URL must not contain user information")
	}

	if serverURL.RawQuery != "" || serverURL.Fragment != "" {
		return errors.New("server URL must not contain a query or fragment")
	}

	if err := auth.ValidateBearerToken(c.BearerToken); err != nil {
		return fmt.Errorf("validate controller credential: %w", err)
	}

	if err := validateImage("checkout image", c.CheckoutImage); err != nil {
		return err
	}

	if err := validateImage("harness image", c.HarnessImage); err != nil {
		return err
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

	return nil
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
