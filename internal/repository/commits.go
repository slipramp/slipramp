package repository

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/google/uuid"
)

const commitURLPlaceholder = "{sha}"

type CommitKey struct {
	RepositoryID uuid.UUID
	SHA          string
}

type Commit struct {
	RepositoryID RepositoryID
	SHA          string
	Title        *string
	AuthorName   *string
	AuthoredAt   *time.Time
	CommittedAt  *time.Time
	URL          *string
}

type CommitHistoryIndexStatus string

const (
	CommitHistoryIndexPending   CommitHistoryIndexStatus = "pending"
	CommitHistoryIndexSucceeded CommitHistoryIndexStatus = "succeeded"
	CommitHistoryIndexFailed    CommitHistoryIndexStatus = "failed"
)

type CommitHistoryIndex struct {
	Status          CommitHistoryIndexStatus
	DesiredHeadSHA  *string
	IndexedHeadSHA  *string
	CommitCount     int
	LastAttemptedAt *time.Time
	LastSucceededAt *time.Time
	FailureReason   *string
}

type CommitHistoryFilter struct {
	RepositoryID *RepositoryID
	Query        *string
	Limit        int
	Offset       int
}

type IndexedCommit struct {
	RepositoryID   RepositoryID
	RepositoryName string
	Commit         Commit
}

type CommitHistoryPage struct {
	Commits []IndexedCommit
	Total   int
}

type CommitReader interface {
	ListCommits(context.Context, []CommitKey) (map[CommitKey]Commit, error)
	ListCommitHistory(context.Context, CommitHistoryFilter) (CommitHistoryPage, error)
}

func validateCommitURLTemplate(value *string) error {
	if value == nil {
		return nil
	}
	if len(*value) > maximumCloneURLLength {
		return fmt.Errorf("must not exceed %d characters", maximumCloneURLLength)
	}
	if containsWhitespaceOrControl(*value) {
		return errors.New("must not contain whitespace or control characters")
	}
	if strings.Count(*value, commitURLPlaceholder) != 1 {
		return fmt.Errorf("must contain exactly one %s placeholder", commitURLPlaceholder)
	}

	resolved := strings.Replace(*value, commitURLPlaceholder, strings.Repeat("a", 40), 1)
	parsed, err := url.Parse(resolved)
	if err != nil {
		return fmt.Errorf("parse: %w", err)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return errors.New("scheme must be https or http")
	}
	if parsed.Hostname() == "" {
		return errors.New("host must not be empty")
	}
	if parsed.User != nil {
		return errors.New("embedded credentials are not supported")
	}

	return nil
}

func CommitURL(cfg Config, sha string) *string {
	if !validCommitSHA(sha) {
		return nil
	}
	if cfg.CommitURLTemplate != nil {
		value := strings.Replace(*cfg.CommitURLTemplate, commitURLPlaceholder, sha, 1)

		return &value
	}

	baseURL, path, ok := repositoryWebLocation(cfg.CloneURL)
	if !ok {
		return nil
	}

	var commitPath string
	switch strings.ToLower(baseURL.Hostname()) {
	case "github.com", "codeberg.org", "gitea.com":
		commitPath = "commit/"
	case "gitlab.com":
		commitPath = "-/commit/"
	case "bitbucket.org":
		commitPath = "commits/"
	default:
		return nil
	}

	baseURL.Path = "/" + strings.Trim(path, "/") + "/" + commitPath + sha

	value := baseURL.String()

	return &value
}

func repositoryWebLocation(cloneURL string) (*url.URL, string, bool) {
	if strings.Contains(cloneURL, "://") {
		parsed, err := url.Parse(cloneURL)
		if err != nil || parsed.Hostname() == "" || parsed.Port() != "" {
			return nil, "", false
		}

		return &url.URL{Scheme: "https", Host: parsed.Hostname()}, repositoryWebPath(parsed.Path), true
	}

	remote, path, found := strings.Cut(cloneURL, ":")
	if !found {
		return nil, "", false
	}
	if _, host, hasUser := strings.Cut(remote, "@"); hasUser {
		remote = host
	}
	if remote == "" || path == "" {
		return nil, "", false
	}

	return &url.URL{Scheme: "https", Host: remote}, repositoryWebPath(path), true
}

func repositoryWebPath(path string) string {
	return strings.TrimSuffix(strings.Trim(path, "/"), ".git")
}
