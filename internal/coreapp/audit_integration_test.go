//go:build postgres

package coreapp

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestAuditDatesAndExport(t *testing.T) {
	s, _, handler, owner := coreFixture(t)
	ctx := context.Background()
	const password = "audit-export-password-123"
	admin, err := s.BootstrapAdmin(ctx, BootstrapInput{Email: "admin@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	tenant, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Audit export"})
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateTenant(ctx, admin, CreateTenantInput{Name: "Other"})
	if err != nil {
		t.Fatal(err)
	}
	session, err := s.Login(ctx, LoginInput{Email: admin.Email, Password: password})
	if err != nil {
		t.Fatal(err)
	}
	call := func(path, token string, status int) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest("GET", "/api/v1/tenants/"+path, nil)
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		if w.Code != status {
			data := w.Body.String()
			if len(data) > 512 {
				data = data[:512]
			}
			t.Fatalf("GET %s: %d want %d: %s", path, w.Code, status, data)
		}
		return w
	}
	readCSV := func(w *httptest.ResponseRecorder) [][]string {
		t.Helper()
		if !strings.HasPrefix(w.Body.String(), "\xef\xbb\xbf") {
			t.Fatal("missing UTF-8 BOM")
		}
		if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("Content-Type") != "text/csv; charset=utf-8" || !strings.HasPrefix(w.Header().Get("Content-Disposition"), "attachment; filename=\"amsonia-audit-") {
			t.Fatalf("bad download headers: %+v", w.Header())
		}
		if w.Header().Get("Content-Length") != strconv.Itoa(w.Body.Len()) {
			t.Fatal("wrong content length")
		}
		rows, err := csv.NewReader(strings.NewReader(strings.TrimPrefix(w.Body.String(), "\xef\xbb\xbf"))).ReadAll()
		if err != nil {
			t.Fatal(err)
		}
		return rows
	}
	start := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	end := start.Add(time.Hour)
	insert := func(target string, at time.Time, outcome string) int64 {
		t.Helper()
		var id int64
		err := owner.QueryRow(ctx, `INSERT INTO amsonia.audit_events(tenant_id,actor_subject,host_initiator,operation,phase,target_type,target_id,outcome,reason_code,request_id,role_version,at)
		 VALUES($1,'=actor','宿主','fixture.audit','result','role',$2,$3,'  @reason','request',7,$4) RETURNING id`, tenant.ID, target, outcome, at).Scan(&id)
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	insert("before", start.Add(-time.Microsecond), "success")
	first := insert("a,\"b\"\n文字", start, "success")
	second := insert("＝formula", start.Add(time.Microsecond), "success")
	insert("denied", start.Add(30*time.Minute), "denied")
	insert("at-end", end, "success")
	insert("after", end.Add(time.Microsecond), "success")
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.audit_events(tenant_id,operation,target_type,target_id,outcome,at) VALUES($1,'fixture.audit','role','foreign','success',$2)`, other.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	query := "?operation=fixture.audit&outcome=success&from=" + url.QueryEscape("2026-10-25T08:00:00+08:00") + "&until=" + url.QueryEscape(end.Format(time.RFC3339Nano))
	base := tenant.ID + "/audit-events"
	var page AuditPage
	if err := json.Unmarshal(call(base+query+"&limit=1", session.AccessToken, 200).Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != second || page.NextCursor != second {
		t.Fatalf("wrong first page: %+v", page)
	}
	var next AuditPage
	json.Unmarshal(call(base+query+"&limit=1&before="+strconv.FormatInt(page.NextCursor, 10), session.AccessToken, 200).Body.Bytes(), &next)
	if len(next.Items) != 1 || next.Items[0].ID != first || next.NextCursor != 0 {
		t.Fatalf("wrong next page: %+v", next)
	}
	rows := readCSV(call(base+"/export"+query, session.AccessToken, 200))
	if len(rows) != 3 || rows[1][0] != strconv.FormatInt(second, 10) || rows[2][0] != strconv.FormatInt(first, 10) {
		t.Fatalf("export differs from date-filtered pages: %+v", rows)
	}
	if rows[1][3] != "\t=actor" || rows[1][4] != "宿主" || rows[1][8] != "\t＝formula" || rows[1][10] != "\t  @reason" || rows[2][8] != "a,\"b\"\n文字" {
		t.Fatalf("CSV encoding or text guard failed: %+v", rows)
	}
	if len(readCSV(call(base+"/export?operation=absent", session.AccessToken, 200))) != 1 {
		t.Fatal("empty export must contain its header")
	}
	for _, suffix := range []string{"?from=bad", "?until=", "?from=2026-10-25T00:00:00", "?from=2026-10-25T01:00:00Z&until=2026-10-25T00:00:00Z", "?from=2026-10-25T00:00:00Z&until=2026-10-25T00:00:00Z", "?from=0000-01-01T00:00:00Z", "?q=%00"} {
		call(base+suffix, session.AccessToken, 400)
		call(base+"/export"+suffix, session.AccessToken, 400)
	}
	call(base+"/export?before=1", session.AccessToken, 400)
	call(base+"/export?limit=30", session.AccessToken, 400)
	call(base+"/export", "", 401)
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.accounts(account_id,email,normalized_email,password_hash) SELECT 'viewer','viewer@example.test','viewer@example.test',password_hash FROM amsonia.accounts WHERE account_id=$1`, admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.tenant_memberships(tenant_id,account_id) VALUES($1,'viewer')`, tenant.ID)
	if err != nil {
		t.Fatal(err)
	}
	viewer, err := s.Login(ctx, LoginInput{Email: "viewer@example.test", Password: password})
	if err != nil {
		t.Fatal(err)
	}
	call(base+"/export?from=bad", viewer.AccessToken, 403)
	call(other.ID+"/audit-events/export", viewer.AccessToken, 404)
	otherRows := readCSV(call(other.ID+"/audit-events/export"+query, session.AccessToken, 200))
	if len(otherRows) != 2 || otherRows[1][8] != "foreign" {
		t.Fatalf("tenant leak: %+v", otherRows)
	}

	// Block the export's SELECT after its signed serializable transaction has
	// started, then commit a concurrent append. That append must not enter the
	// already established snapshot, but must appear in the next download.
	insert("snapshot-first", start, "success")
	locker, err := owner.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer locker.Rollback(ctx)
	if _, err := locker.Exec(ctx, `LOCK amsonia.audit_events IN ACCESS EXCLUSIVE MODE`); err != nil {
		t.Fatal(err)
	}
	type exportResult struct {
		data []byte
		err  error
	}
	completed := make(chan exportResult, 1)
	go func() {
		data, err := s.auditExport(ctx, tenant.ID, AuditFilter{Query: "snapshot-", Operation: "fixture.audit"})
		completed <- exportResult{data, err}
	}()
	deadline := time.Now().Add(5 * time.Second)
	for {
		var blocked bool
		err := owner.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND pid<>pg_backend_pid() AND state='active' AND wait_event_type='Lock' AND query LIKE '%FROM amsonia.audit_events%')`).Scan(&blocked)
		if err != nil {
			t.Fatal(err)
		}
		if blocked {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("export did not reach snapshot barrier")
		}
		time.Sleep(10 * time.Millisecond)
	}
	_, err = locker.Exec(ctx, `INSERT INTO amsonia.audit_events(tenant_id,operation,target_type,target_id,outcome,at) VALUES($1,'fixture.audit','role','snapshot-late','success',$2)`, tenant.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	if err := locker.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-completed:
		if result.err != nil || strings.Contains(string(result.data), "snapshot-late") || !strings.Contains(string(result.data), "snapshot-first") {
			t.Fatalf("inconsistent snapshot: %v", result.err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("export did not finish")
	}
	if rows := readCSV(call(base+"/export?q=snapshot-&operation=fixture.audit", session.AccessToken, 200)); len(rows) != 3 {
		t.Fatalf("next snapshot missed append: %d rows", len(rows))
	}

	// Exact row limit is valid; one more row and oversize bytes fail as JSON.
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.audit_events(tenant_id,operation,target_type,target_id,outcome,at) SELECT $1,'fixture.bulk','role',i::text,'success',$2 FROM generate_series(1,10000) i`, tenant.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	if rows := readCSV(call(base+"/export?operation=fixture.bulk", session.AccessToken, 200)); len(rows) != 10001 {
		t.Fatalf("row limit truncated: %d", len(rows))
	}
	_, err = owner.Exec(ctx, `INSERT INTO amsonia.audit_events(tenant_id,operation,target_type,target_id,outcome,at) VALUES($1,'fixture.bulk','role','extra','success',$2),($1,'fixture.bytes','role',repeat('x',10485760),'success',$2)`, tenant.ID, start)
	if err != nil {
		t.Fatal(err)
	}
	for _, operation := range []string{"fixture.bulk", "fixture.bytes"} {
		res := call(base+"/export?operation="+operation, session.AccessToken, 422)
		if res.Header().Get("Content-Disposition") != "" || !strings.HasPrefix(res.Header().Get("Content-Type"), "application/json") || !strings.Contains(res.Body.String(), "audit_export_too_large") {
			t.Fatalf("partial CSV response for %s", operation)
		}
	}
}
