package coreapp

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/willunylabs/amsonia-core"
)

func TestDirectoryValidation(t *testing.T) {
	cursor := directoryCursor{Tenant: "tenant", Kind: "invitations", Query: "收件人", Status: "pending", ID: "invite_a", At: time.Now().UTC()}
	data, _ := json.Marshal(cursor)
	token := base64.RawURLEncoding.EncodeToString(data)
	valid := DirectoryFilter{Query: " 收件人 ", Status: "pending", After: token, Limit: 2}
	f, got, err := directoryInput("tenant", "invitations", valid)
	if err != nil || f.Query != "收件人" || got.ID != cursor.ID {
		t.Fatalf("valid cursor: %+v %+v %v", f, got, err)
	}
	for name, input := range map[string]DirectoryFilter{
		"malformed": {After: "bad!"}, "oversized cursor": {After: strings.Repeat("a", maxDirectoryCursorSize+1)},
		"negative limit": {Limit: -1}, "large limit": {Limit: 101},
		"long query": {Query: strings.Repeat("字", 129)}, "invalid utf8": {Query: "\xff"}, "nul": {Query: "a\x00b"},
		"unknown status": {Status: "disabled"}, "changed query": {Query: "other", Status: "pending", After: token},
		"changed status": {Query: "收件人", After: token},
	} {
		t.Run(name, func(t *testing.T) {
			if _, _, err := directoryInput("tenant", "invitations", input); !errors.Is(err, amsonia.ErrInvalidInput) {
				t.Fatalf("accepted invalid filter: %v", err)
			}
		})
	}
	for _, target := range []struct{ tenant, kind string }{{"other", "invitations"}, {"tenant", "members"}} {
		if _, _, err := directoryInput(target.tenant, target.kind, valid); !errors.Is(err, amsonia.ErrInvalidInput) {
			t.Fatalf("accepted foreign cursor: %v", err)
		}
	}
	if _, _, err := directoryInput("tenant", "roles", DirectoryFilter{Status: "active"}); !errors.Is(err, amsonia.ErrInvalidInput) {
		t.Fatal("roles accepted status filter")
	}
	for _, raw := range []string{"0", "-1", "101", "bad", ""} {
		w := httptest.NewRecorder()
		if _, ok := directoryQuery(w, httptest.NewRequest("GET", "/?limit="+raw, nil)); ok || w.Code != 400 {
			t.Fatalf("accepted invalid HTTP limit %q", raw)
		}
	}
}

func TestRoleMemberCursorEscapedIDs(t *testing.T) {
	tenant, role, account, query := strings.Repeat("<", 256), strings.Repeat("&", 256), strings.Repeat(">", 256), strings.Repeat("<", 128)
	cursor := directoryCursor{Tenant: tenant, Kind: "role-members", Query: query, Role: role, Assignment: "assigned"}
	filter := DirectoryFilter{Query: query, Limit: 1}
	page := DirectoryPage[Member]{Items: []Member{{AccountID: account}, {AccountID: "next"}}}
	finishDirectory(&page, filter, cursor, func(m Member) (string, time.Time) { return m.AccountID, time.Time{} })
	filter.After = page.NextCursor
	_, decoded, err := directoryInput(tenant, "role-members", filter)
	if err != nil || decoded.Role != role || decoded.ID != account || decoded.Assignment != "assigned" {
		t.Fatalf("valid escaped IDs cannot be paged: size %d, %v", len(page.NextCursor), err)
	}
}
