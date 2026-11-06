package coreapp

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDecodeJSONPreservesValidUnicodeAndRejectsInvalidEncoding(t *testing.T) {
	tests := []struct {
		name   string
		body   string
		valid  bool
		wantID string
	}{
		{name: "literal replacement character", body: "{\"id\":\"租户-\ufffd\"}", valid: true, wantID: "租户-\ufffd"},
		{name: "escaped replacement character", body: `{"id":"id-\ufffd"}`, valid: true, wantID: "id-\ufffd"},
		{name: "surrogate pair", body: `{"id":"\ud83d\ude00"}`, valid: true, wantID: "😀"},
		{name: "trailing whitespace", body: "{\"id\":\"reader\"}\n\t ", valid: true, wantID: "reader"},
		{name: "invalid UTF-8", body: "{\"id\":\"id-\xff\"}"},
		{name: "truncated UTF-8", body: "{\"id\":\"id-\xe2\x82\"}"},
		{name: "null object", body: `null`},
		{name: "array", body: `[]`},
		{name: "unknown field", body: `{"id":"reader","extra":true}`},
		{name: "multiple JSON values", body: `{"id":"reader"} {"id":"admin"}`},
		{name: "body limit", body: `{"id":"` + strings.Repeat("a", 1<<20) + `"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.body))
			response := httptest.NewRecorder()
			var input struct {
				ID string `json:"id"`
			}
			if valid := decodeJSON(response, request, &input); valid != tt.valid {
				t.Fatalf("decode accepted = %t; want %t", valid, tt.valid)
			}
			if tt.valid {
				if input.ID != tt.wantID {
					t.Errorf("decoded ID = %q; want exact %q", input.ID, tt.wantID)
				}
			} else if response.Code != http.StatusBadRequest {
				t.Errorf("response status = %d; want 400", response.Code)
			}
		})
	}
}
