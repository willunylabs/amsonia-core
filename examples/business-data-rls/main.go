// Command business-data-rls proves that a host application's table can share
// Amsonia Core's signed PostgreSQL tenant boundary.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/postgres"
)

type invoice struct {
	ID          string
	Description string
	AmountCents int64
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	dsn := strings.TrimSpace(os.Getenv("AMSONIA_DATABASE_DSN"))
	if dsn == "" {
		return errors.New("AMSONIA_DATABASE_DSN is required")
	}
	secret, err := base64.RawURLEncoding.DecodeString(strings.TrimSpace(os.Getenv("AMSONIA_TENANT_BINDING_SECRET")))
	if err != nil || len(secret) < 32 {
		return errors.New("AMSONIA_TENANT_BINDING_SECRET must be unpadded base64url encoding of at least 32 random bytes")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect runtime database: %w", err)
	}
	defer pool.Close()
	store, err := postgres.NewStore(pool, secret)
	if err != nil {
		return err
	}

	fixtures := map[amsonia.TenantID]invoice{
		"tenant-acme":   {ID: "inv-acme-001", Description: "Acme platform subscription", AmountCents: 4900},
		"tenant-globex": {ID: "inv-globex-001", Description: "Globex platform subscription", AmountCents: 9900},
	}
	for tenantID, item := range fixtures {
		if err := upsertInvoice(ctx, store, tenantID, item); err != nil {
			return fmt.Errorf("seed %s: %w", tenantID, err)
		}
	}
	for tenantID := range fixtures {
		items, err := listInvoices(ctx, store, tenantID)
		if err != nil {
			return fmt.Errorf("list %s: %w", tenantID, err)
		}
		if len(items) != 1 || items[0].ID != fixtures[tenantID].ID {
			return fmt.Errorf("tenant isolation failed for %s: visible invoices=%v", tenantID, items)
		}
		fmt.Printf("%s sees only %s (query has no tenant WHERE clause)\n", tenantID, items[0].ID)
	}

	err = store.RunTenant(ctx, "tenant-acme", false, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO app.invoices (tenant_id, invoice_id, description, amount_cents)
			VALUES ('tenant-globex', 'inv-forged', 'must be rejected', 1)
		`)
		return err
	})
	if err == nil {
		return errors.New("cross-tenant insert unexpectedly succeeded")
	}
	fmt.Println("cross-tenant insert rejected by PostgreSQL RLS")
	return nil
}

func upsertInvoice(ctx context.Context, store *postgres.Store, tenantID amsonia.TenantID, item invoice) error {
	return store.RunTenant(ctx, tenantID, false, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `
			INSERT INTO app.invoices (tenant_id, invoice_id, description, amount_cents)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (tenant_id, invoice_id) DO UPDATE
			SET description = EXCLUDED.description, amount_cents = EXCLUDED.amount_cents
		`, tenantID, item.ID, item.Description, item.AmountCents)
		return err
	})
}

func listInvoices(ctx context.Context, store *postgres.Store, tenantID amsonia.TenantID) ([]invoice, error) {
	var result []invoice
	err := store.RunTenant(ctx, tenantID, true, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT invoice_id, description, amount_cents FROM app.invoices ORDER BY invoice_id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item invoice
			if err := rows.Scan(&item.ID, &item.Description, &item.AmountCents); err != nil {
				return err
			}
			result = append(result, item)
		}
		return rows.Err()
	})
	return result, err
}
