// Command api runs the standalone Amsonia Core HTTP API.
package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/willunylabs/amsonia-core"
	"github.com/willunylabs/amsonia-core/internal/coreapp"
	"github.com/willunylabs/amsonia-core/postgres"
)

type hostAuthority struct{}

func (hostAuthority) AuthorizeBootstrap(ctx context.Context, tenantID amsonia.TenantID) (amsonia.HostProvenance, error) {
	return amsonia.HostProvenance{Initiator: "core-system-administrator", At: time.Now().UTC()}, nil
}

type noMemberships struct{}

func (noMemberships) LookupWorkspaceMembership(context.Context, amsonia.TenantID, amsonia.WorkspaceID, amsonia.SubjectID) (amsonia.WorkspaceMembership, error) {
	return amsonia.WorkspaceMembership{}, amsonia.ErrNotFound
}

type logAudit struct{ logger *slog.Logger }

func (audit logAudit) RecordSecurityEvent(_ context.Context, event amsonia.MutationAuditEvent) error {
	audit.logger.Warn("security audit", "tenant", event.TenantID, "operation", event.Operation, "outcome", event.Outcome, "reason", event.ReasonCode)
	return nil
}

func main() {
	if len(os.Args) == 2 && os.Args[1] == "healthcheck" {
		client := &http.Client{Timeout: 3 * time.Second}
		response, err := client.Get("http://127.0.0.1:8080/readyz")
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	dsn := os.Getenv("AMSONIA_DATABASE_DSN")
	if dsn == "" {
		return errors.New("AMSONIA_DATABASE_DSN is required")
	}
	bindingSecret, err := decodeSecret("AMSONIA_TENANT_BINDING_SECRET")
	if err != nil {
		return err
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return fmt.Errorf("connect database: %w", err)
	}
	defer pool.Close()
	startupContext, cancelStartup := context.WithTimeout(ctx, 10*time.Second)
	defer cancelStartup()
	if err := postgres.VerifyMigrationState(startupContext, pool); err != nil {
		return fmt.Errorf("database is not ready: %w", err)
	}
	store, err := postgres.NewStore(pool, bindingSecret)
	if err != nil {
		return err
	}
	catalog, controls, err := coreapp.CoreCatalog()
	if err != nil {
		return err
	}
	audit := logAudit{logger: slog.Default()}
	bootstrap, err := amsonia.NewBootstrapper(catalog, store, controls, hostAuthority{}, amsonia.RealClock{})
	if err != nil {
		return err
	}
	manager, err := amsonia.NewManager(catalog, store, noMemberships{}, controls, audit, amsonia.RealClock{})
	if err != nil {
		return err
	}
	authorizer, err := amsonia.NewAuthorizer(catalog, store, noMemberships{})
	if err != nil {
		return err
	}
	service, err := coreapp.NewService(pool, store, bootstrap)
	if err != nil {
		return err
	}
	api, err := coreapp.NewAPI(service, manager, authorizer, catalog, slog.Default())
	if err != nil {
		return err
	}
	address := strings.TrimSpace(os.Getenv("AMSONIA_HTTP_ADDR"))
	if address == "" {
		address = ":8080"
	}
	server := &http.Server{Addr: address, Handler: api.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 32 << 10}
	serverContext, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-serverContext.Done()
		shutdownContext, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownContext)
	}()
	slog.Info("Amsonia Core API listening", "address", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func decodeSecret(name string) ([]byte, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return nil, fmt.Errorf("%s is required", name)
	}
	secret, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil || len(secret) < 32 {
		return nil, fmt.Errorf("%s must be unpadded base64url encoding of at least 32 random bytes", name)
	}
	return secret, nil
}
