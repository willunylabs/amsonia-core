//go:build postgres

package coreapp

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/willunylabs/amsonia-core"
)

func TestSignedContextsPreserveRetryableDatabaseErrors(t *testing.T) {
	s, _, _, owner := coreFixture(t)
	ctx := context.Background()
	// Inject failures precisely inside signature verification, in an isolated
	// disposable database. Existing bind calls still use the real HMAC first.
	_, err := owner.Exec(ctx, `ALTER FUNCTION public.hmac(bytea,bytea,text) RENAME TO context_test_hmac;
 CREATE FUNCTION public.hmac(data bytea,key bytea,kind text) RETURNS bytea LANGUAGE plpgsql AS $$
 DECLARE failure text := nullif(current_setting('amsonia_test.sqlstate',true),'');
 BEGIN
 IF failure IS NOT NULL THEN RAISE EXCEPTION 'injected context verification error' USING ERRCODE=failure; END IF;
 RETURN public.context_test_hmac(data,key,kind);
 END; $$;`)
	if err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []string{"tenant", "actor"} {
		for _, state := range []string{"", "40001", "40P01", "22000"} {
			t.Run(purpose+"/"+state, func(t *testing.T) {
				var value *string
				read := func(tx pgx.Tx) error {
					if _, err := tx.Exec(ctx, `SELECT set_config('amsonia_test.sqlstate',$1,true)`, state); err != nil {
						return err
					}
					return tx.QueryRow(ctx, `SELECT amsonia.`+purpose+`_id()`).Scan(&value)
				}
				var err error
				if purpose == "tenant" {
					err = s.store.RunTenant(ctx, amsonia.TenantID("signed-context-test"), true, read)
				} else {
					err = s.store.RunActor(ctx, "signed-context-test", read)
				}
				if state == "40001" || state == "40P01" {
					var databaseError *pgconn.PgError
					if !errors.As(err, &databaseError) || databaseError.Code != state {
						t.Fatalf("retryable conflict was masked: value=%v error=%v", value, err)
					}
				} else if err != nil {
					t.Fatal(err)
				} else if state == "" && (value == nil || *value != "signed-context-test") {
					t.Fatalf("valid signature rejected: %v", value)
				} else if state == "22000" && value != nil {
					t.Fatalf("failed verification did not fail closed: %q", *value)
				}
			})
		}
	}
}

func TestParallelTenantInvitationsKeepRetryableConflicts(t *testing.T) {
	s, _, _, _ := coreFixture(t)
	ctx := context.Background()
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "parallel@example.test", Password: "parallel-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	a, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Parallel A"})
	if err != nil {
		t.Fatal(err)
	}
	b, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Parallel B"})
	if err != nil {
		t.Fatal(err)
	}
	for iteration := range 8 {
		results := make(chan error, 2)
		for _, tenant := range []Tenant{a, b} {
			go func() {
				_, err := s.CreateInvitation(ctx, tenant.ID, admin.ID, fmt.Sprintf("parallel-%d@example.test", iteration))
				results <- err
			}()
		}
		for range 2 {
			err := <-results
			if err == nil {
				continue
			}
			var databaseError *pgconn.PgError
			if !errors.As(err, &databaseError) || databaseError.Code != "40001" {
				t.Fatalf("parallel tenant write was misclassified: %v", err)
			}
		}
	}
}
