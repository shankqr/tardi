package db

import (
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestIsUniqueViolation_WithCode23505(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505"}
	if !IsUniqueViolation(pgErr) {
		t.Error("expected IsUniqueViolation to return true for code 23505")
	}
}

func TestIsUniqueViolation_WithDifferentCode(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23503"} // foreign key violation
	if IsUniqueViolation(pgErr) {
		t.Error("expected IsUniqueViolation to return false for code 23503")
	}
}

func TestIsUniqueViolation_WithNonPgError(t *testing.T) {
	err := errors.New("some random error")
	if IsUniqueViolation(err) {
		t.Error("expected IsUniqueViolation to return false for non-PG error")
	}
}

func TestIsUniqueViolation_WithWrappedPgError(t *testing.T) {
	pgErr := &pgconn.PgError{Code: "23505"}
	wrapped := errors.Join(errors.New("wrapper"), pgErr)
	if !IsUniqueViolation(wrapped) {
		t.Error("expected IsUniqueViolation to return true for wrapped PG error with code 23505")
	}
}

func TestIsUniqueViolation_NilError(t *testing.T) {
	if IsUniqueViolation(nil) {
		t.Error("expected IsUniqueViolation to return false for nil error")
	}
}

func TestErrConflict(t *testing.T) {
	if ErrConflict.Error() != "conflict: precondition failed" {
		t.Errorf("ErrConflict message = %q", ErrConflict.Error())
	}
}

func TestErrLimitReached(t *testing.T) {
	if ErrLimitReached.Error() != "limit reached" {
		t.Errorf("ErrLimitReached message = %q", ErrLimitReached.Error())
	}
}
