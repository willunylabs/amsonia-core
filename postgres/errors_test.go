package postgres

import (
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/willunylabs/amsonia-core"
)

func TestWrapPgxErrUsesSQLState(t *testing.T) {
	duplicate := &pgconn.PgError{Code: "23505", Message: "重复键违反唯一约束"}
	wrapped := fmt.Errorf("insert role: %w", duplicate)
	got := wrapPgxErr(wrapped)
	if !errors.Is(got, amsonia.ErrConflict) || !errors.Is(got, wrapped) {
		t.Fatalf("unique violation lost classification or cause: %v", got)
	}
	var cause *pgconn.PgError
	if !errors.As(got, &cause) || cause != duplicate {
		t.Fatalf("driver error lost: %v", got)
	}
	for _, err := range []error{
		errors.New("connection closed while reporting duplicate key"),
		&pgconn.PgError{Code: "08006", Message: "duplicate key text from an unrelated failure"},
	} {
		if got := wrapPgxErr(err); got != err {
			t.Errorf("non-unique failure classified as conflict: %v", got)
		}
	}
	if wrapPgxErr(nil) != nil || !errors.Is(wrapPgxErr(pgx.ErrNoRows), amsonia.ErrNotFound) {
		t.Fatal("nil or missing-row behavior changed")
	}
}
