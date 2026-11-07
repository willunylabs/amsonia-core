//go:build postgres

package coreapp

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestLoginRechecksLockoutAfterPasswordVerification(t *testing.T) {
	s, _, _, owner := coreFixture(t)
	ctx := context.Background()
	const password = "concurrent-login-password"
	account, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "lockout@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	locker, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback(ctx)
	if _, err := locker.Exec(ctx, `SELECT account_id FROM amsonia.accounts WHERE account_id=$1 FOR UPDATE`, account.ID); err != nil {
		t.Fatal(err)
	}
	pending, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { _, err := s.Login(pending, LoginInput{Email: account.Email, Password: password}); done <- err }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		if err := owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE 'SELECT password_hash, status%FOR UPDATE')`).Scan(&blocked); err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("login did not reach account lock after password verification")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Commit exactly the lockout state produced by the fifth failed attempt,
	// after the correct-password request has read the earlier unlocked state.
	if _, err := locker.Exec(ctx, `UPDATE amsonia.accounts SET failed_login_count=0,locked_until=now()+interval '15 minutes' WHERE account_id=$1`, account.ID); err != nil {
		t.Fatal(err)
	}
	if err := locker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrAccountLocked) {
		t.Fatalf("newly locked account issued a session: %v", err)
	}
	var locked bool
	var failures, sessions int
	if err := owner.QueryRow(ctx, `SELECT locked_until>now(),failed_login_count,(SELECT count(*) FROM amsonia.access_sessions WHERE account_id=$1) FROM amsonia.accounts WHERE account_id=$1`, account.ID).Scan(&locked, &failures, &sessions); err != nil || !locked || failures != 0 || sessions != 0 {
		t.Fatalf("lockout changed: locked=%t failures=%d sessions=%d err=%v", locked, failures, sessions, err)
	}
}
