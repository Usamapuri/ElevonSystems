package util

import (
	"errors"
	"strings"

	"github.com/lib/pq"
)

// IsUniqueViolation reports whether err is Postgres SQLSTATE 23505 raised by
// the named constraint or index, so handlers can answer 409 with a code
// instead of a 500 carrying a driver string.
func IsUniqueViolation(err error, constraint string) bool {
	if err == nil {
		return false
	}
	var pqErr *pq.Error
	if errors.As(err, &pqErr) {
		return pqErr.Code == "23505" && (pqErr.Constraint == constraint || strings.Contains(pqErr.Message, constraint))
	}
	return false
}
