package repository

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/slipramp/slipramp/internal/kubevalidate"
)

const (
	maximumCloneURLLength = 2048
	maximumBranchLength   = 255
)

var (
	ErrInvalidConfig = errors.New("invalid repository configuration")
	ErrNameConflict  = errors.New("repository name already exists")
	ErrNotFound      = errors.New("repository not found")
)

type Repository struct {
	Config
	ID        RepositoryID
	CreatedAt time.Time
	Refresh   RefreshState
}

type RepositoryID = uuid.UUID

type Config struct {
	Name                   string
	CloneURL               string
	Branch                 string
	RefreshCredentialName  *string
	CheckoutCredentialName *string
}

func (c Config) Validate() error {
	if err := kubevalidate.DNSLabel("repository name", c.Name); err != nil {
		return err
	}

	if err := validateCloneURL(c.CloneURL); err != nil {
		return fmt.Errorf("clone URL: %w", err)
	}

	if err := validateBranch(c.Branch); err != nil {
		return fmt.Errorf("branch: %w", err)
	}

	if err := validateCredential(
		"refresh credential name",
		c.RefreshCredentialName,
	); err != nil {
		return err
	}

	if err := validateCredential(
		"checkout credential name",
		c.CheckoutCredentialName,
	); err != nil {
		return err
	}

	return nil
}

func validateCloneURL(cloneURL string) error {
	if cloneURL == "" {
		return errors.New("must not be empty")
	}

	if len(cloneURL) > maximumCloneURLLength {
		return fmt.Errorf("must not exceed %d characters", maximumCloneURLLength)
	}

	if containsWhitespaceOrControl(cloneURL) {
		return errors.New("must not contain whitespace or control characters")
	}

	if strings.Contains(cloneURL, "://") {
		return validateAbsoluteCloneURL(cloneURL)
	}

	return validateSCPCloneURL(cloneURL)
}

func validateAbsoluteCloneURL(cloneURL string) error {
	parsed, err := url.Parse(cloneURL)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}

	scheme := strings.ToLower(parsed.Scheme)
	if scheme != "https" && scheme != "ssh" {
		return errors.New("scheme must be https or ssh")
	}

	if parsed.Hostname() == "" {
		return errors.New("host must not be empty")
	}

	if parsed.Path == "" || parsed.Path == "/" {
		return errors.New("repository path must not be empty")
	}

	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("query strings and fragments are not supported")
	}

	if parsed.User != nil {
		if _, hasPassword := parsed.User.Password(); hasPassword {
			return errors.New("embedded passwords are not supported")
		}

		if scheme == "https" {
			return errors.New("embedded HTTPS credentials are not supported")
		}
	}

	return nil
}

func validateSCPCloneURL(cloneURL string) error {
	remote, path, found := strings.Cut(cloneURL, ":")
	if !found || remote == "" || path == "" {
		return errors.New("must be an HTTPS, SSH, or SCP-style Git URL")
	}

	host := remote
	if user, parsedHost, hasUser := strings.Cut(remote, "@"); hasUser {
		if user == "" || strings.ContainsAny(user, "/:@") {
			return errors.New("SCP-style URL has an invalid user")
		}

		host = parsedHost
	}

	if host == "" || strings.ContainsAny(host, "/:@") {
		return errors.New("SCP-style URL has an invalid host")
	}

	return nil
}

func validateBranch(branch string) error {
	if branch == "" {
		return errors.New("must not be empty")
	}

	if len(branch) > maximumBranchLength {
		return fmt.Errorf("must not exceed %d characters", maximumBranchLength)
	}

	if containsWhitespaceOrControl(branch) ||
		branch == "@" ||
		strings.HasPrefix(branch, "-") ||
		strings.HasPrefix(branch, "/") ||
		strings.HasSuffix(branch, "/") ||
		strings.HasSuffix(branch, ".") ||
		strings.Contains(branch, "..") ||
		strings.Contains(branch, "@{") ||
		strings.ContainsAny(branch, " ~^:?*[\\") {
		return errors.New("must be a valid Git branch name")
	}

	for component := range strings.SplitSeq(branch, "/") {
		if component == "" || strings.HasPrefix(component, ".") ||
			strings.HasSuffix(component, ".lock") {
			return errors.New("must be a valid Git branch name")
		}
	}

	return nil
}

func containsWhitespaceOrControl(value string) bool {
	return strings.IndexFunc(value, func(character rune) bool {
		return unicode.IsSpace(character) || unicode.IsControl(character)
	}) >= 0
}

func validateCredential(label string, credential *string) error {
	if credential == nil {
		return nil
	}

	return kubevalidate.DNSLabel(label, *credential)
}
