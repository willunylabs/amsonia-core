//go:build postgres

package coreapp

import (
	"context"
	"testing"
)

func TestSessionRetention(t *testing.T) {
	s, _, _, owner := coreFixture(t)
	ctx := context.Background()
	account, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "prune@example.test", Password: "prune-test-password"})
	if err != nil {
		t.Fatal(err)
	}
	var sessions []Session
	for range 5 {
		v, e := s.Login(ctx, LoginInput{Email: account.Email, Password: "prune-test-password"})
		if e != nil {
			t.Fatal(e)
		}
		sessions = append(sessions, v)
	}
	for i, v := range sessions {
		age := 40
		if i == 3 {
			age = 1
		}
		if i == 4 {
			age = -1
		}
		for _, table := range []string{"access_sessions", "refresh_sessions"} {
			if _, err := owner.Exec(ctx, `UPDATE amsonia.`+table+` SET expires_at=now()-make_interval(days=>$1) WHERE account_id=$2 AND session_id=(SELECT session_id FROM amsonia.access_sessions WHERE token_hash=$3)`, age, account.ID, tokenHash(v.AccessToken)); err != nil {
				t.Fatal(err)
			}
		}
	}
	opts := SessionPruneOptions{RetentionDays: 30, BatchSize: 2}
	dry, err := PruneSessions(ctx, owner, opts)
	if err != nil || dry.AccessSessions != 2 || dry.RefreshSessions != 2 || dry.Applied {
		t.Fatalf("preview %#v %v", dry, err)
	}
	count := func(table string) int {
		t.Helper()
		var n int
		if err := owner.QueryRow(ctx, `SELECT count(*) FROM amsonia.`+table).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	if count("access_sessions") != 5 || count("refresh_sessions") != 5 {
		t.Fatal("preview mutated data")
	}
	opts.Apply = true
	if _, err := PruneSessions(ctx, s.pool, opts); err == nil {
		t.Fatal("runtime role deleted credentials")
	}
	tx, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `SELECT 1 FROM amsonia.access_sessions WHERE token_hash=$1 FOR UPDATE`, tokenHash(sessions[0].AccessToken)); err != nil {
		t.Fatal(err)
	}
	applied, err := PruneSessions(ctx, owner, opts)
	if err != nil || applied.AccessSessions != 2 || applied.RefreshSessions != 2 {
		t.Fatalf("apply %#v %v", applied, err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if count("access_sessions") != 3 || count("refresh_sessions") != 3 {
		t.Fatal("unbounded delete")
	}
	applied, err = PruneSessions(ctx, owner, opts)
	if err != nil || applied.AccessSessions != 1 || applied.RefreshSessions != 1 {
		t.Fatalf("remaining %#v %v", applied, err)
	}
	if count("access_sessions") != 2 || count("refresh_sessions") != 2 {
		t.Fatal("grace or live session deleted")
	}
	if _, err := s.Authenticate(ctx, sessions[4].AccessToken); err != nil {
		t.Fatal("live session affected", err)
	}
	for _, bad := range []SessionPruneOptions{{RetentionDays: 0, BatchSize: 2}, {RetentionDays: 30, BatchSize: 5001}} {
		if _, err := PruneSessions(ctx, owner, bad); err == nil {
			t.Fatal("invalid options accepted")
		}
	}
}
