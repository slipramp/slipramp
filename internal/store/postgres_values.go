package store

import (
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgtype"
)

func databaseTime(value time.Time) pgtype.Timestamptz {
	return pgtype.Timestamptz{Time: value, Valid: true}
}

func optionalDatabaseTime(value *time.Time) pgtype.Timestamptz {
	if value == nil {
		return pgtype.Timestamptz{}
	}

	return databaseTime(*value)
}

func applicationTime(value pgtype.Timestamptz) *time.Time {
	if !value.Valid {
		return nil
	}

	converted := value.Time.UTC()

	return &converted
}

func nullableText(value string) *string {
	if value == "" {
		return nil
	}

	return &value
}

func textValue(value *string) string {
	if value == nil {
		return ""
	}

	return *value
}

func cloneStringPointer(value *string) *string {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}

func databaseUUID(value uuid.UUID) pgtype.UUID {
	return pgtype.UUID{Bytes: value, Valid: true}
}

func optionalDatabaseUUID(value *uuid.UUID) pgtype.UUID {
	if value == nil {
		return pgtype.UUID{}
	}

	return databaseUUID(*value)
}

func applicationUUID(value pgtype.UUID) *uuid.UUID {
	if !value.Valid {
		return nil
	}

	converted := uuid.UUID(value.Bytes)

	return &converted
}
