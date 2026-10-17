package controller

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/slipramp/slipramp/internal/auth"
	"github.com/slipramp/slipramp/internal/cluster"
)

type Config struct {
	ClusterName string
	ServerURL   string
	BearerToken string
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

	return nil
}
