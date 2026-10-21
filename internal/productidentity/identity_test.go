package productidentity

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPrincipalDisplayNameLengthUsesCharacters(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.October, 20, 12, 0, 0, 0, time.UTC)
	principal := Principal{
		ID:          uuid.New(),
		Kind:        KindUser,
		DisplayName: strings.Repeat("é", maximumDisplayNameLength),
		Role:        RoleViewer,
		CreatedAt:   now,
		UpdatedAt:   now,
	}
	if err := principal.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}

	principal.DisplayName += "é"
	if err := principal.Validate(); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Validate() error = %v, want invalid identity", err)
	}
}
