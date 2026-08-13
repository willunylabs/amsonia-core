// Command amsonia provides operator workflows for Amsonia Core.
package main

import (
	"bufio"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/term"

	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/internal/coreapp"
	"github.com/willunylabs/amsonia-core/postgres"
)

func main() {
	if err := run(context.Background(), os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) != 1 || (args[0] != "migrate" && args[0] != "migration-status" && args[0] != "bootstrap-admin") {
		return errors.New("usage: amsonia migrate | migration-status | bootstrap-admin")
	}
	dsnName := "AMSONIA_MIGRATION_DSN"
	if args[0] == "bootstrap-admin" {
		dsnName = "AMSONIA_DATABASE_DSN"
	}
	dsn := os.Getenv(dsnName)
	if dsn == "" {
		return fmt.Errorf("%s is required", dsnName)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect migration database: %w", err)
	}
	defer pool.Close()
	switch args[0] {
	case "migrate":
		return postgres.Migrate(ctx, pool)
	case "migration-status":
		states, err := postgres.MigrationStates(ctx, pool)
		if err != nil {
			return err
		}
		for _, state := range states {
			fmt.Printf("%06d  %-32s  dirty=%-5t  %s\n", state.Version, state.Name, state.Dirty, state.AppliedAt.UTC().Format(time.RFC3339))
		}
	case "bootstrap-admin":
		return bootstrapAdmin(ctx, pool)
	}
	return nil
}

func bootstrapAdmin(ctx context.Context, pool *pgxpool.Pool) error {
	reader := bufio.NewReader(os.Stdin)
	fmt.Fprint(os.Stdout, "Administrator email: ")
	email, err := reader.ReadString('\n')
	if err != nil {
		return fmt.Errorf("read email: %w", err)
	}
	if !term.IsTerminal(int(syscall.Stdin)) {
		return errors.New("bootstrap-admin requires an interactive terminal")
	}
	fmt.Fprint(os.Stdout, "Password: ")
	password, err := term.ReadPassword(int(syscall.Stdin))
	fmt.Fprintln(os.Stdout)
	if err != nil {
		return fmt.Errorf("read password: %w", err)
	}
	// Bootstrap only uses the identity tables, but NewService deliberately
	// requires the complete runtime composition. Build a minimal safe
	// bootstrapper and Store using the configured tenant binding.
	secret, err := decodeBindingSecret()
	if err != nil {
		return err
	}
	store, err := postgres.NewStore(pool, secret)
	if err != nil {
		return err
	}
	catalog, _ := amsonia.NewCatalog([]amsonia.PermissionDefinition{
		{Key: "iam:role:manage"}, {Key: "iam:grant:manage"}, {Key: "iam:role:assign"}, {Key: "iam:member:manage"}, {Key: "iam:audit:read"},
	})
	controls := amsonia.ControlPermissions{ManageRoles: "iam:role:manage", ManageGrants: "iam:grant:manage", AssignRoles: "iam:role:assign"}
	bootstrapper, err := amsonia.NewBootstrapper(catalog, store, controls, cliHostAuthority{}, amsonia.RealClock{})
	if err != nil {
		return err
	}
	service, err := coreapp.NewService(pool, store, bootstrapper)
	if err != nil {
		return err
	}
	account, err := service.BootstrapAdmin(ctx, coreapp.BootstrapInput{Email: strings.TrimSpace(email), Password: string(password)})
	for index := range password {
		password[index] = 0
	}
	if err != nil {
		return err
	}
	fmt.Fprintf(os.Stdout, "Created system administrator %s (%s)\n", account.Email, account.ID)
	return nil
}

type cliHostAuthority struct{}

func (cliHostAuthority) AuthorizeBootstrap(context.Context, amsonia.TenantID) (amsonia.HostProvenance, error) {
	return amsonia.HostProvenance{Initiator: "amsonia-cli", At: time.Now().UTC()}, nil
}

func decodeBindingSecret() ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv("AMSONIA_TENANT_BINDING_SECRET"))
	secret, err := base64.RawURLEncoding.DecodeString(raw)
	if raw == "" || err != nil || len(secret) < 32 {
		return nil, errors.New("AMSONIA_TENANT_BINDING_SECRET must be unpadded base64url encoding of at least 32 random bytes")
	}
	return secret, nil
}
