package gate

import (
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/slipramp/slipramp/internal/kubevalidate"
)

type Origin string
type ScopeKind string

const (
	RepositoryOrigin Origin    = "repository"
	ServerOrigin     Origin    = "server"
	RepositoryScope  ScopeKind = "repository"
)

// Scope identifies the owner of a policy independently of its display name.
type Scope struct {
	Kind ScopeKind
	ID   uuid.UUID
}

type Key string
type Version string

// Reference identifies a Gate and one exact policy version within a subject.
type Reference struct {
	Key     Key
	Version Version
}

func RepositoryKey(name string) Key {
	return Key("repository/" + name)
}

func (key Key) validRepositoryKey() bool {
	name, ok := strings.CutPrefix(string(key), "repository/")
	return ok && kubevalidate.DNSLabel("Gate name", name) == nil
}

func (version Version) valid() bool {
	value := string(version)
	return value != "" && utf8.RuneCountInString(value) <= 128 &&
		strings.TrimSpace(value) == value && !strings.ContainsRune(value, 0)
}

func LockKey(scopeID uuid.UUID) Key {
	return Key("lock/" + scopeID.String())
}

func WindowKey(scopeID uuid.UUID) Key {
	return Key("window/" + scopeID.String())
}

func (key Key) ServerScope() (kind string, id uuid.UUID, ok bool) {
	kind, text, found := strings.Cut(string(key), "/")
	if !found || (kind != LockType && kind != WindowType) {
		return "", uuid.Nil, false
	}
	id, err := uuid.Parse(text)
	return kind, id, err == nil && id != uuid.Nil && id.String() == text
}
