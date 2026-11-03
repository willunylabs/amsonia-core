package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/willunylabs/amsonia-core/internal/coreapp"
	"github.com/willunylabs/amsonia-core/postgres"
	"golang.org/x/term"
)

func recoverAdmin(ctx context.Context, pool *pgxpool.Pool) error {
	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		return errors.New("recover-admin requires an interactive terminal")
	}
	if err := postgres.VerifyMigrationState(ctx, pool); err != nil {
		return err
	}
	fmt.Fprint(os.Stdout, "Administrator email: ")
	email, err := bufio.NewReader(os.Stdin).ReadString('\n')
	if err != nil {
		return fmt.Errorf("read administrator email: %w", err)
	}
	readPassword := func(prompt string) ([]byte, error) {
		fmt.Fprint(os.Stdout, prompt)
		value, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stdout)
		return value, err
	}
	password, err := readPassword("New password (revokes all existing sessions): ")
	defer clear(password)
	if err != nil {
		return fmt.Errorf("read new password: %w", err)
	}
	confirm, err := readPassword("Confirm new password: ")
	defer clear(confirm)
	if err != nil {
		return fmt.Errorf("read password confirmation: %w", err)
	}
	if !bytes.Equal(password, confirm) {
		return errors.New("passwords do not match")
	}
	id, err := coreapp.RecoverAdministrator(ctx, pool, strings.TrimSpace(email), string(password))
	if err != nil {
		return fmt.Errorf("recover active system administrator: %w", err)
	}
	fmt.Printf("Recovered administrator %s; existing sessions revoked.\n", id)
	return nil
}
