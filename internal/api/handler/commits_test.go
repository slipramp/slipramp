package handler

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/api"
	"github.com/slipramp/slipramp/internal/repository"
)

func TestCommitResponsesEnrichKnownCommitsAndKeepFallbacks(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	knownKey := repository.CommitKey{
		RepositoryID: repositoryID,
		SHA:          "0123456789abcdef0123456789abcdef01234567",
	}
	unknownKey := repository.CommitKey{
		RepositoryID: repositoryID,
		SHA:          "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	title := "Improve commit references"
	author := "Slipramp Test"
	authoredAt := time.Date(2026, time.October, 22, 18, 0, 0, 0, time.UTC)
	url := "https://github.com/example/ops/commit/" + knownKey.SHA
	handler := Handler{commits: staticCommitService{
		knownKey: {
			RepositoryID: knownKey.RepositoryID,
			SHA:          knownKey.SHA,
			Title:        &title,
			AuthorName:   &author,
			AuthoredAt:   &authoredAt,
			URL:          &url,
		},
	}}

	commits, err := handler.commitResponses(t.Context(), []repository.CommitKey{knownKey, unknownKey})
	if err != nil {
		t.Fatalf("commitResponses() error = %v", err)
	}
	if commits[knownKey].Title == nil || *commits[knownKey].Title != title ||
		commits[knownKey].Author == nil || *commits[knownKey].Author != author ||
		commits[knownKey].AuthoredAt == nil || !commits[knownKey].AuthoredAt.Equal(authoredAt) ||
		commits[knownKey].URL == nil || *commits[knownKey].URL != url {
		t.Errorf("known commit = %#v", commits[knownKey])
	}
	if commits[unknownKey].Sha != unknownKey.SHA || commits[unknownKey].Title != nil {
		t.Errorf("unknown commit = %#v", commits[unknownKey])
	}
}

func TestListCommitsReturnsIndexedHistory(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	sha := "0123456789abcdef0123456789abcdef01234567"
	title := "Index the complete branch"
	committedAt := time.Date(2026, time.October, 22, 18, 30, 0, 0, time.UTC)
	service := &recordingCommitService{
		page: repository.CommitHistoryPage{
			Commits: []repository.IndexedCommit{{
				RepositoryID:   repositoryID,
				RepositoryName: "infrastructure",
				Commit: repository.Commit{
					RepositoryID: repositoryID,
					SHA:          sha,
					Title:        &title,
					CommittedAt:  &committedAt,
				},
			}},
			Total: 1,
		},
	}
	query := "Index"
	limit := 10
	offset := int32(20)
	handler := Handler{commits: service}
	response, err := handler.ListCommits(t.Context(), api.ListCommitsRequestObject{
		Params: api.ListCommitsParams{
			RepositoryID: &repositoryID,
			Q:            &query,
			Limit:        &limit,
			Offset:       &offset,
		},
	})
	if err != nil {
		t.Fatalf("ListCommits() error = %v", err)
	}

	result, ok := response.(api.ListCommits200JSONResponse)
	if !ok {
		t.Fatalf("response = %#v", response)
	}
	if result.Total != 1 || result.Limit != limit || result.Offset != int(offset) ||
		len(result.Items) != 1 || result.Items[0].RepositoryID != repositoryID ||
		result.Items[0].Commit.Sha != sha || result.Items[0].Commit.CommittedAt == nil ||
		!result.Items[0].Commit.CommittedAt.Equal(committedAt) {
		t.Errorf("response = %#v", result)
	}
	if service.filter.RepositoryID == nil || *service.filter.RepositoryID != repositoryID ||
		service.filter.Query == nil || *service.filter.Query != query ||
		service.filter.Limit != limit || service.filter.Offset != int(offset) {
		t.Errorf("filter = %#v", service.filter)
	}
}

func TestGetCommitReturnsKnownCommit(t *testing.T) {
	t.Parallel()

	repositoryID := uuid.MustParse("018f90ff-9fd8-7b5f-b7e1-f32ff54c1200")
	sha := "0123456789abcdef0123456789abcdef01234567"
	title := "Add commit detail"
	service := &recordingCommitService{
		item: repository.IndexedCommit{
			RepositoryID:   repositoryID,
			RepositoryName: "infrastructure",
			Commit: repository.Commit{
				RepositoryID: repositoryID,
				SHA:          sha,
				Title:        &title,
			},
		},
	}
	handler := Handler{commits: service}

	response, err := handler.GetCommit(t.Context(), api.GetCommitRequestObject{
		RepositoryID: repositoryID,
		CommitSha:    sha,
	})
	if err != nil {
		t.Fatalf("GetCommit() error = %v", err)
	}

	result, ok := response.(api.GetCommit200JSONResponse)
	if !ok {
		t.Fatalf("response = %#v", response)
	}
	if service.key.RepositoryID != repositoryID || service.key.SHA != sha ||
		result.RepositoryID != repositoryID || result.RepositoryName != "infrastructure" ||
		result.Commit.Sha != sha || result.Commit.Title == nil || *result.Commit.Title != title {
		t.Errorf("response = %#v, key = %#v", result, service.key)
	}
}

type staticCommitService map[repository.CommitKey]repository.Commit

func (s staticCommitService) ListCommits(
	_ context.Context,
	_ []repository.CommitKey,
) (map[repository.CommitKey]repository.Commit, error) {
	return s, nil
}

func (s staticCommitService) GetCommit(
	_ context.Context,
	key repository.CommitKey,
) (repository.IndexedCommit, error) {
	commit, ok := s[key]
	if !ok {
		return repository.IndexedCommit{}, repository.ErrCommitNotFound
	}

	return repository.IndexedCommit{RepositoryID: key.RepositoryID, Commit: commit}, nil
}

func (staticCommitService) ListCommitHistory(
	context.Context,
	repository.CommitHistoryFilter,
) (repository.CommitHistoryPage, error) {
	return repository.CommitHistoryPage{}, nil
}

type recordingCommitService struct {
	filter repository.CommitHistoryFilter
	page   repository.CommitHistoryPage
	key    repository.CommitKey
	item   repository.IndexedCommit
	err    error
}

func (*recordingCommitService) ListCommits(
	context.Context,
	[]repository.CommitKey,
) (map[repository.CommitKey]repository.Commit, error) {
	return nil, nil
}

func (s *recordingCommitService) GetCommit(
	_ context.Context,
	key repository.CommitKey,
) (repository.IndexedCommit, error) {
	s.key = key

	return s.item, s.err
}

func (s *recordingCommitService) ListCommitHistory(
	_ context.Context,
	filter repository.CommitHistoryFilter,
) (repository.CommitHistoryPage, error) {
	s.filter = filter

	return s.page, nil
}
