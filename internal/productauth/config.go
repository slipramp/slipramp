package productauth

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/slipramp/slipramp/internal/productidentity"
)

const (
	DefaultSessionLifetime          = 12 * time.Hour
	DefaultLoginTransactionLifetime = 10 * time.Minute
	oidcCallbackPath                = "/api/v1/auth/callback"
)

type OIDCConfig struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
	SubjectRoles map[string]productidentity.Role
}

type Config struct {
	OIDC                     OIDCConfig
	SessionLifetime          time.Duration
	LoginTransactionLifetime time.Duration
}

func DefaultConfig() Config {
	return Config{
		OIDC: OIDCConfig{
			SubjectRoles: map[string]productidentity.Role{},
		},
		SessionLifetime:          DefaultSessionLifetime,
		LoginTransactionLifetime: DefaultLoginTransactionLifetime,
	}
}

func (c OIDCConfig) Validate() error {
	if c.Issuer == "" && c.ClientID == "" && c.ClientSecret == "" &&
		c.RedirectURL == "" && len(c.SubjectRoles) == 0 {
		return errors.New("OIDC configuration is required for product authentication")
	}

	for _, field := range []struct {
		label string
		value string
	}{
		{label: "issuer", value: c.Issuer},
		{label: "client ID", value: c.ClientID},
		{label: "client secret", value: c.ClientSecret},
		{label: "redirect URL", value: c.RedirectURL},
	} {
		if strings.TrimSpace(field.value) == "" {
			return fmt.Errorf("OIDC %s is required", field.label)
		}
	}

	if err := validateHTTPURL("OIDC issuer", c.Issuer); err != nil {
		return err
	}
	if err := validateHTTPURL("OIDC redirect URL", c.RedirectURL); err != nil {
		return err
	}
	redirectURL, _ := url.Parse(c.RedirectURL)
	if redirectURL.Path != oidcCallbackPath {
		return fmt.Errorf("OIDC redirect URL path must be %q", oidcCallbackPath)
	}
	if len(c.SubjectRoles) == 0 {
		return errors.New("OIDC subject roles must enroll at least one subject")
	}

	hasAdministrator := false
	for subject, role := range c.SubjectRoles {
		if strings.TrimSpace(subject) == "" || subject != strings.TrimSpace(subject) {
			return errors.New("OIDC enrolled subjects must not be blank or have surrounding whitespace")
		}
		if role != productidentity.RoleAdmin &&
			role != productidentity.RoleOperator &&
			role != productidentity.RoleViewer {
			return fmt.Errorf("OIDC subject %q has invalid role %q", subject, role)
		}
		if role == productidentity.RoleAdmin {
			hasAdministrator = true
		}
	}
	if !hasAdministrator {
		return errors.New("OIDC subject roles must enroll at least one administrator")
	}

	return nil
}

func (c Config) Validate() error {
	if err := c.OIDC.Validate(); err != nil {
		return err
	}
	if c.SessionLifetime <= 0 {
		return errors.New("product session lifetime must be positive")
	}
	if c.LoginTransactionLifetime <= 0 {
		return errors.New("OIDC login transaction lifetime must be positive")
	}
	return nil
}

func validateHTTPURL(label, value string) error {
	parsed, err := url.Parse(value)
	if err != nil || !parsed.IsAbs() || parsed.Host == "" {
		return fmt.Errorf("%s must be an absolute URL", label)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return fmt.Errorf("%s must use HTTP or HTTPS", label)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must not contain user information, a query, or a fragment", label)
	}

	return nil
}
