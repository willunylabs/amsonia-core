//go:build postgres

package coreapp

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestDeviceLogoutAndRefreshLifecycle(t *testing.T) {
	s, _, handler, _ := coreFixture(t)
	ctx := context.Background()
	account, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "session@example.test", Password: "session-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	login := func() Session {
		t.Helper()
		session, err := s.Login(ctx, LoginInput{Email: account.Email, Password: "session-test-password"})
		if err != nil {
			t.Fatal(err)
		}
		return session
	}
	refresh := func(session Session) Session {
		t.Helper()
		next, err := s.Refresh(ctx, RefreshInput{RefreshToken: session.RefreshToken})
		if err != nil {
			t.Fatal(err)
		}
		return next
	}
	assertInvalid := func(token string) {
		t.Helper()
		if _, err := s.Authenticate(ctx, token); !errors.Is(err, ErrSessionInvalid) {
			t.Fatalf("access still valid: %v", err)
		}
	}
	first := login()
	other := login()
	rotated := refresh(first)
	current := refresh(rotated)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/logout", nil)
	req.Header.Set("Authorization", "Bearer "+first.AccessToken)
	req.AddCookie(&http.Cookie{Name: "amsonia_refresh", Value: current.RefreshToken})
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusNoContent || !strings.Contains(res.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("logout %d %s", res.Code, res.Body.String())
	}
	for _, session := range []Session{first, rotated, current} {
		assertInvalid(session.AccessToken)
	}
	if _, err := s.Refresh(ctx, RefreshInput{RefreshToken: current.RefreshToken}); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("revoked refresh: %v", err)
	}
	if _, err := s.Authenticate(ctx, other.AccessToken); err != nil {
		t.Fatalf("unrelated device revoked: %v", err)
	}
	other = refresh(other)
	// Public token-specific service entry points follow the same family contract.
	onlyAccess := login()
	afterRotation := refresh(onlyAccess)
	if err := s.Logout(ctx, onlyAccess.AccessToken); err != nil {
		t.Fatal(err)
	}
	assertInvalid(afterRotation.AccessToken)
	onlyRefresh := login()
	afterRefresh := refresh(onlyRefresh)
	if err := s.RevokeRefresh(ctx, afterRefresh.RefreshToken); err != nil {
		t.Fatal(err)
	}
	assertInvalid(onlyRefresh.AccessToken)
	assertInvalid(afterRefresh.AccessToken)
	if _, err := s.Authenticate(ctx, other.AccessToken); err != nil {
		t.Fatal(err)
	}
	// Account-first locking makes every valid refresh/logout ordering safe.
	for range 5 {
		session := login()
		var next Session
		var rotationError, logoutError error
		var group sync.WaitGroup
		group.Add(2)
		go func() {
			defer group.Done()
			next, rotationError = s.Refresh(ctx, RefreshInput{RefreshToken: session.RefreshToken})
		}()
		go func() {
			defer group.Done()
			logoutError = s.logoutSessions(ctx, session.AccessToken, session.RefreshToken)
		}()
		group.Wait()
		if logoutError != nil {
			t.Fatal(logoutError)
		}
		var databaseError *pgconn.PgError
		if rotationError != nil && !errors.Is(rotationError, ErrSessionInvalid) && !(errors.As(rotationError, &databaseError) && databaseError.Code == "40001") {
			t.Fatalf("unexpected parallel refresh error: %v", rotationError)
		}
		assertInvalid(session.AccessToken)
		if rotationError == nil {
			assertInvalid(next.AccessToken)
		}
		var active int
		if err := s.pool.QueryRow(ctx, `SELECT count(*) FROM (
    SELECT session_id FROM amsonia.access_sessions WHERE token_hash=$1
  ) family JOIN amsonia.refresh_sessions r USING(session_id)
  WHERE r.revoked_at IS NULL AND r.expires_at>now()`, tokenHash(session.AccessToken)).Scan(&active); err != nil || active != 0 {
			t.Fatalf("refresh resurrected a logged-out device: %d %v", active, err)
		}
		if _, err := s.Authenticate(ctx, other.AccessToken); err != nil {
			t.Fatalf("parallel logout affected another device: %v", err)
		}
	}
	// Commit a rotation without delivering its replacement cookie to the
	// client. The retry presents the same old cookie, which is treated as reuse.
	replay := login()
	refreshHTTP := func() *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
		r.AddCookie(&http.Cookie{Name: "amsonia_refresh", Value: replay.RefreshToken})
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	lost := refreshHTTP()
	var latest Session
	if lost.Code != http.StatusOK || json.Unmarshal(lost.Body.Bytes(), &latest) != nil || latest.AccessToken == "" ||
		len(lost.Result().Cookies()) != 1 || lost.Result().Cookies()[0].Value == replay.RefreshToken {
		t.Fatalf("rotation did not issue a replacement: %d %s", lost.Code, lost.Body.String())
	}
	denied := refreshHTTP()
	if denied.Code != http.StatusUnauthorized || !strings.Contains(denied.Body.String(), `"code":"refresh_invalid"`) ||
		!strings.Contains(denied.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("old cookie retry: %d %s", denied.Code, denied.Body.String())
	}
	assertInvalid(latest.AccessToken)
	assertInvalid(other.AccessToken)
}

func TestRefreshRechecksExpiryAfterAccountLock(t *testing.T) {
	s, _, _, owner := coreFixture(t)
	ctx := context.Background()
	const password = "refresh-expiry-password"
	account, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "refresh-expiry@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	var timestamp atomic.Int64
	timestamp.Store(time.Now().Unix())
	s.now = func() time.Time { return time.Unix(timestamp.Load(), 0).UTC() }
	session, err := s.Login(ctx, LoginInput{Email: account.Email, Password: password})
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
	go func() {
		_, err := s.Refresh(pending, RefreshInput{RefreshToken: session.RefreshToken})
		done <- err
	}()
	waitForAccountLock(t, owner, "SELECT account_id FROM amsonia.accounts%WHERE account_id=(SELECT account_id FROM amsonia.refresh_sessions%FOR UPDATE")
	timestamp.Add(int64(s.refresh / time.Second))
	if err := locker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("refresh expired while waiting for the lock: %v", err)
	}
	var expired bool
	if err := owner.QueryRow(ctx, `SELECT revoked_at IS NOT NULL AND rotated_to_hash IS NULL FROM amsonia.refresh_sessions WHERE token_hash=$1`, tokenHash(session.RefreshToken)).Scan(&expired); err != nil || !expired {
		t.Fatalf("expired token was not marked revoked without rotation: %t %v", expired, err)
	}
}

func TestSessionDeviceMetadataSurvivesPruning(t *testing.T) {
	s, _, _, owner := coreFixture(t)
	ctx := context.Background()
	const password = "session-metadata-password"
	account, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "metadata@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	started := time.Now().UTC().Truncate(time.Second)
	now := started
	s.now = func() time.Time { return now }
	first, err := s.Login(ctx, LoginInput{
		Email: account.Email, Password: password, RemoteAddress: "192.0.2.99", UserAgent: "Alpha",
	})
	if err != nil {
		t.Fatal(err)
	}
	now = started.Add(time.Minute)
	next, err := s.Refresh(ctx, RefreshInput{
		RefreshToken: first.RefreshToken, RemoteAddress: "192.0.2.10", UserAgent: "Zulu",
	})
	if err != nil {
		t.Fatal(err)
	}
	list := func(token string) AccountSession {
		t.Helper()
		sessions, err := s.ListSessions(ctx, account.ID, token)
		if err != nil || len(sessions) != 1 || !sessions[0].Current {
			t.Fatalf("expected one current family: %+v %v", sessions, err)
		}
		item := sessions[0]
		if !item.CreatedAt.Equal(started) || !item.LastSeenAt.Equal(now) ||
			!item.ExpiresAt.Equal(now.Add(s.refresh)) ||
			item.RemoteAddress != "192.0.2.10" || item.UserAgent != "Zulu" {
			t.Fatalf("unexpected session metadata: %+v", item)
		}
		return item
	}
	before := list(first.AccessToken)
	for table, token := range map[string]string{"access_sessions": first.AccessToken, "refresh_sessions": first.RefreshToken} {
		if _, err := owner.Exec(ctx, `UPDATE amsonia.`+table+` SET expires_at=now()-interval '40 days' WHERE token_hash=$1`, tokenHash(token)); err != nil {
			t.Fatal(err)
		}
	}
	pruned, err := PruneSessions(ctx, owner, SessionPruneOptions{RetentionDays: 30, BatchSize: 10, Apply: true})
	if err != nil || pruned.AccessSessions != 1 || pruned.RefreshSessions != 1 {
		t.Fatalf("prune original tokens: %+v %v", pruned, err)
	}
	if after := list(next.AccessToken); before.ID != after.ID {
		t.Fatalf("session ID changed from %s to %s", before.ID, after.ID)
	}
}

func TestRefreshServiceFailurePreservesCookie(t *testing.T) {
	s, _, handler, _ := coreFixture(t)
	s.pool.Close() // deterministic unavailable service, distinct from invalid credentials
	unavailable := httptest.NewRequest(http.MethodPost, "/api/v1/auth/restore", nil)
	unavailable.AddCookie(&http.Cookie{Name: "amsonia_refresh", Value: "rs_" + strings.Repeat("a", 50)})
	restoreError := httptest.NewRecorder()
	handler.ServeHTTP(restoreError, unavailable)
	if restoreError.Code != http.StatusInternalServerError || restoreError.Header().Get("Set-Cookie") != "" {
		t.Fatalf("restore service failure deleted cookie: %d", restoreError.Code)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil)
	req.AddCookie(&http.Cookie{Name: "amsonia_refresh", Value: "rs_" + strings.Repeat("a", 50)})
	res := httptest.NewRecorder()
	handler.ServeHTTP(res, req)
	if res.Code != http.StatusInternalServerError || res.Header().Get("Set-Cookie") != "" {
		t.Fatalf("service failure deleted cookie: %d %s", res.Code, res.Header().Get("Set-Cookie"))
	}
	missing := httptest.NewRecorder()
	handler.ServeHTTP(missing, httptest.NewRequest(http.MethodPost, "/api/v1/auth/refresh", nil))
	if missing.Code != http.StatusUnauthorized || !strings.Contains(missing.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("missing cookie: %d %s", missing.Code, missing.Header().Get("Set-Cookie"))
	}
}

func TestOptionalSessionRestore(t *testing.T) {
	s, _, handler, _ := coreFixture(t)
	ctx := context.Background()
	restore := func(token string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/restore", nil)
		if token != "" {
			req.AddCookie(&http.Cookie{Name: "amsonia_refresh", Value: token})
		}
		res := httptest.NewRecorder()
		handler.ServeHTTP(res, req)
		return res
	}
	for _, token := range []string{"", "rs_" + strings.Repeat("a", 50)} {
		res := restore(token)
		if res.Code != http.StatusNoContent || res.Body.Len() != 0 {
			t.Fatalf("anonymous restore: %d %s", res.Code, res.Body.String())
		}
	}
	account, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "restore@example.test", Password: "restore-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, LoginInput{Email: account.Email, Password: "restore-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	res := restore(session.RefreshToken)
	if res.Code != http.StatusOK {
		t.Fatalf("restore: %d %s", res.Code, res.Body.String())
	}
	var restored Session
	if err := json.Unmarshal(res.Body.Bytes(), &restored); err != nil {
		t.Fatal(err)
	}
	if restored.Account.ID != account.ID || restored.AccessToken == "" || restored.RefreshToken != "" {
		t.Fatalf("restored payload %#v", restored)
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 || !cookies[0].HttpOnly || cookies[0].Value == session.RefreshToken || cookies[0].Path != "/api/v1/auth" {
		t.Fatalf("cookie rotation: %#v", cookies)
	}
	if _, err := s.Authenticate(ctx, restored.AccessToken); err != nil {
		t.Fatal(err)
	}
	// The optional endpoint suppresses expected authentication errors, not replay protection.
	replay := restore(session.RefreshToken)
	if replay.Code != http.StatusNoContent {
		t.Fatalf("replay restore: %d", replay.Code)
	}
	if _, err := s.Authenticate(ctx, restored.AccessToken); !errors.Is(err, ErrSessionInvalid) {
		t.Fatalf("replay response weakened: %v", err)
	}
}
