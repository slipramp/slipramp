package catalog

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"
)

const (
	PresentationTitleMaxLength       = 200
	PresentationDescriptionMaxLength = 2000
	DocumentationURLMaxLength        = 2048
)

func validatePresentationTitle(subject, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have leading or trailing whitespace", subject)
	}
	if strings.ContainsAny(value, "\r\n") {
		return fmt.Errorf("%s must be a single line", subject)
	}
	if utf8.RuneCountInString(value) > PresentationTitleMaxLength {
		return fmt.Errorf("%s must not exceed %d characters", subject, PresentationTitleMaxLength)
	}

	return nil
}

func validatePresentationDescription(subject, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have leading or trailing whitespace", subject)
	}
	if utf8.RuneCountInString(value) > PresentationDescriptionMaxLength {
		return fmt.Errorf("%s must not exceed %d characters", subject, PresentationDescriptionMaxLength)
	}

	return nil
}

func validateDocumentationURL(subject, value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value {
		return fmt.Errorf("%s must not have leading or trailing whitespace", subject)
	}
	if utf8.RuneCountInString(value) > DocumentationURLMaxLength {
		return fmt.Errorf("%s must not exceed %d characters", subject, DocumentationURLMaxLength)
	}

	parsed, err := url.Parse(value)
	if err != nil || parsed.Host == "" || parsed.User != nil ||
		(parsed.Scheme != "http" && parsed.Scheme != "https") {
		return fmt.Errorf("%s must be an absolute HTTP or HTTPS URL", subject)
	}

	return nil
}
