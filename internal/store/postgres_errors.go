package store

import (
	"errors"
	"slices"

	"github.com/jackc/pgx/v5/pgconn"
)

func constraintMatches(err error, constraints ...string) bool {
	var postgresError *pgconn.PgError
	if !errors.As(err, &postgresError) {
		return false
	}

	return slices.Contains(constraints, postgresError.ConstraintName)
}
