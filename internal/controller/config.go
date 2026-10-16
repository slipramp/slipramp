package controller

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

type Config struct {
	ClusterName string
	ServerURL   string
	BearerToken string
}

func (c Config) Validate() error {
	if strings.TrimSpace(c.ClusterName) == "" {
		return errors.New("cluster name must not be empty")
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

	if strings.TrimSpace(c.BearerToken) == "" {
		return errors.New("bearer token must not be empty")
	}

	return nil
}
