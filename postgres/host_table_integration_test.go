//go:build postgres

package postgres

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/willunylabs/amsonia-core"
)

func TestHostBusinessTableUsesSignedTenantBoundary(t *testing.T) {
	store := setupPostgres(t)
	admin := newPool(t, adminURL(t))
	ctx := context.Background()
	if _, err := admin.Exec(ctx, `
		DROP SCHEMA IF EXISTS amsonia_host_example CASCADE;
		CREATE SCHEMA amsonia_host_example;
		CREATE TABLE amsonia_host_example.invoices (
			tenant_id TEXT NOT NULL,
			invoice_id TEXT NOT NULL,
			amount_cents BIGINT NOT NULL,
			PRIMARY KEY (tenant_id, invoice_id)
		);
		ALTER TABLE amsonia_host_example.invoices ENABLE ROW LEVEL SECURITY;
		ALTER TABLE amsonia_host_example.invoices FORCE ROW LEVEL SECURITY;
		CREATE POLICY tenant_isolation ON amsonia_host_example.invoices
			USING (tenant_id = amsonia.tenant_id())
			WITH CHECK (tenant_id = amsonia.tenant_id());
		REVOKE ALL ON SCHEMA amsonia_host_example FROM PUBLIC;
		REVOKE ALL ON amsonia_host_example.invoices FROM PUBLIC;
		GRANT USAGE ON SCHEMA amsonia_host_example TO amsonia_test_runtime;
		GRANT SELECT, INSERT, UPDATE, DELETE ON amsonia_host_example.invoices TO amsonia_test_runtime;
	`); err != nil {
		t.Fatalf("create host table: %v", err)
	}
	t.Cleanup(func() {
		_, _ = admin.Exec(context.Background(), "DROP SCHEMA IF EXISTS amsonia_host_example CASCADE")
	})

	for tenantID, invoiceID := range map[amsonia.TenantID]string{
		"host-tenant-a": "invoice-a",
		"host-tenant-b": "invoice-b",
	} {
		err := store.RunTenant(ctx, tenantID, false, func(tx pgx.Tx) error {
			_, err := tx.Exec(ctx, `
				INSERT INTO amsonia_host_example.invoices (tenant_id, invoice_id, amount_cents)
				VALUES ($1, $2, 4200)
			`, tenantID, invoiceID)
			return err
		})
		if err != nil {
			t.Fatalf("insert %s: %v", tenantID, err)
		}
	}

	for tenantID, wantInvoiceID := range map[amsonia.TenantID]string{
		"host-tenant-a": "invoice-a",
		"host-tenant-b": "invoice-b",
	} {
		err := store.RunTenant(ctx, tenantID, true, func(tx pgx.Tx) error {
			var invoiceIDs []string
			rows, err := tx.Query(ctx, `SELECT invoice_id FROM amsonia_host_example.invoices ORDER BY invoice_id`)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var invoiceID string
				if err := rows.Scan(&invoiceID); err != nil {
					return err
				}
				invoiceIDs = append(invoiceIDs, invoiceID)
			}
			if err := rows.Err(); err != nil {
				return err
			}
			if len(invoiceIDs) != 1 || invoiceIDs[0] != wantInvoiceID {
				t.Fatalf("%s saw invoices %v, want [%s]", tenantID, invoiceIDs, wantInvoiceID)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("read %s: %v", tenantID, err)
		}
	}

	err := store.RunTenant(ctx, "host-tenant-a", false, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO amsonia_host_example.invoices (tenant_id, invoice_id, amount_cents)
			VALUES ('host-tenant-b', 'forged', 1)
		`)
		return err
	})
	if err == nil || !strings.Contains(err.Error(), "row-level security") {
		t.Fatalf("cross-tenant insert should fail RLS, got %v", err)
	}

	conn, err := store.pool.Acquire(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, `
		SELECT set_config('amsonia.tenant_id', 'host-tenant-a', false),
		       set_config('amsonia.tenant_txid', txid_current()::text, false),
		       set_config('amsonia.tenant_nonce', '00000000000000000000000000000000', false),
		       set_config('amsonia.tenant_signature', repeat('0', 64), false)
	`); err != nil {
		t.Fatal(err)
	}
	var count int
	if err := conn.QueryRow(ctx, `SELECT count(*) FROM amsonia_host_example.invoices`).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != 0 {
		t.Fatalf("forged tenant context exposed %d host rows", count)
	}
}
