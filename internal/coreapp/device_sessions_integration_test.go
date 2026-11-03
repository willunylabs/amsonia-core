//go:build postgres

package coreapp

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSignOutOtherDevices(t *testing.T) {
	s, _, handler, owner := coreFixture(t)
	ctx := context.Background()
	const password = "device-sessions-password"
	account, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "devices@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	login := func(email string) Session {
		t.Helper()
		session, err := s.Login(ctx, LoginInput{Email: email, Password: password})
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	rotate := func(session Session) Session {
		t.Helper()
		next, err := s.Refresh(ctx, RefreshInput{RefreshToken: session.RefreshToken})
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	invalid := func(token string) {
		t.Helper()
		if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("revoked access accepted: %v", err)
		}
	}
	call := func(token, body string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("POST", "/api/v1/auth/sessions/revoke-others", strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			t.Fatalf("revoke-others: %d want %d: %s", w.Code, status, w.Body.String())
		}
		return w
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.accounts(account_id,email,normalized_email,password_hash) SELECT 'foreign','foreign@example.test','foreign@example.test',password_hash FROM amsonia.accounts WHERE account_id=$1`, account.ID)
	if err != nil {
		t.Fatal(err)
	}
	foreign := login("foreign@example.test")
	first := login(account.Email)
	current := rotate(first)
	other := login(account.Email)
	otherRotated := rotate(other)
	call("", "{}", 401)
	for _, body := range []string{"", `{"account_id":"foreign"}`, `{"session_id":"chosen"}`, `[]`} {
		call(current.AccessToken, body, 400)
	}
	if _, err = s.Authenticate(ctx, otherRotated.AccessToken); err != nil {
		t.Fatal("invalid bodies changed sessions", err)
	}
	res := call(current.AccessToken, "{}", 204)
	if res.Body.Len() != 0 || res.Header().Get("Set-Cookie") != "" {
		t.Fatal("bulk action changed current cookie or response contract")
	}
	for _, session := range []Session{other, otherRotated} {
		invalid(session.AccessToken)
	}
	if _, err = s.Refresh(ctx, RefreshInput{RefreshToken: otherRotated.RefreshToken}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked refresh accepted: %v", err)
	}
	for _, token := range []string{first.AccessToken, current.AccessToken, foreign.AccessToken} {
		if _, err = s.Authenticate(ctx, token); err != nil {
			t.Fatalf("retained session invalid: %v", err)
		}
	}
	call(current.AccessToken, "{}", 204) // idempotent, even without other families
	sessions, err := s.ListSessions(ctx, account.ID, current.AccessToken)
	if err != nil || len(sessions) != 1 || !sessions[0].Current {
		t.Fatalf("current family not retained: %+v %v", sessions, err)
	}
	familyID := sessions[0].ID
	current = rotate(current)
	sessions, err = s.ListSessions(ctx, account.ID, current.AccessToken)
	if err != nil || len(sessions) != 1 || sessions[0].ID != familyID || !sessions[0].Current {
		t.Fatal("current refresh family changed", err)
	}
	if err := s.revokeOtherSessions(ctx, "foreign", current.AccessToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("cross-account bearer accepted: %v", err)
	}
	if err := s.revokeOtherSessions(ctx, account.ID, otherRotated.AccessToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked caller accepted: %v", err)
	}
	expired := login(account.Email)
	if _, err := owner.Exec(ctx, `UPDATE amsonia.access_sessions SET expires_at=now()-interval '1 second' WHERE token_hash=$1`, tokenHash(expired.AccessToken)); err != nil {
		t.Fatal(err)
	}
	if err := s.revokeOtherSessions(ctx, account.ID, expired.AccessToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("expired caller accepted: %v", err)
	}
	call(expired.AccessToken, "{}", 401)

	// A rotation ordered before revocation is included; a later rotation cannot
	// resurrect the other family. Fresh password sign-ins remain allowed.
	for range 5 {
		other := login(account.Email)
		var refreshed Session
		var rotationErr, revokeErr error
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			refreshed, rotationErr = s.Refresh(ctx, RefreshInput{RefreshToken: other.RefreshToken})
		}()
		go func() { defer group.Done(); revokeErr = s.revokeOtherSessions(ctx, account.ID, current.AccessToken) }()
		group.Wait()
		if revokeErr != nil {
			t.Fatal(revokeErr)
		}
		var databaseError *pgconn.PgError
		if rotationErr != nil && !errors.Is(rotationErr, ErrSessionInvalid) && !(errors.As(rotationErr, &databaseError) && databaseError.Code == "40001") {
			t.Fatalf("parallel refresh: %v", rotationErr)
		}
		invalid(other.AccessToken)
		if rotationErr == nil {
			invalid(refreshed.AccessToken)
		}
		var remaining int
		if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.refresh_sessions WHERE account_id=$1 AND revoked_at IS NULL AND expires_at>now()`, account.ID).Scan(&remaining); err != nil || remaining != 1 {
			t.Fatalf("other refresh survived: %d %v", remaining, err)
		}
		if _, err = s.Authenticate(ctx, current.AccessToken); err != nil {
			t.Fatal("current device revoked", err)
		}
	}
	if _, err = s.Authenticate(ctx, foreign.AccessToken); err != nil {
		t.Fatal("other account changed", err)
	}

	// Pause on the account lock, then revoke the caller before releasing it.
	// Authentication must be checked again inside the locked transaction.
	locker, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback(ctx)
	if _, err = locker.Exec(ctx, `SELECT account_id FROM amsonia.accounts WHERE account_id=$1 FOR UPDATE`, account.ID); err != nil {
		t.Fatal(err)
	}
	pendingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.revokeOtherSessions(pendingCtx, account.ID, current.AccessToken) }()
	deadline := time.Now().Add(3 * time.Second)
	for {
		var blocked bool
		err := owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND wait_event_type='Lock' AND query LIKE 'SELECT account_id FROM amsonia.accounts WHERE account_id=$1 AND status=%')`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("action did not reach account lock")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if _, err = locker.Exec(ctx, `UPDATE amsonia.access_sessions SET revoked_at=now() WHERE token_hash=$1`, tokenHash(current.AccessToken)); err != nil {
		t.Fatal(err)
	}
	if err = locker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("stale authentication accepted after lock: %v", err)
	}
}
