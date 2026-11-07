package coreapp

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
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

type observedBody struct {
	io.Reader
	started chan struct{}
}

func (body *observedBody) Read(p []byte) (int, error) {
	select {
	case <-body.started:
	default:
		close(body.started)
	}
	return body.Reader.Read(p)
}

func TestSlowLoginBodyDoesNotOccupyPasswordSlot(t *testing.T) {
	api := &API{service: &Service{}, logger: slog.New(slog.NewTextHandler(io.Discard, nil)), loginSlots: make(chan struct{}, 1)}
	handler := api.Handler()
	reader, writer := io.Pipe()
	started := make(chan struct{})
	done := make(chan struct{})
	defer reader.Close()
	slow := httptest.NewRequest("POST", "/api/v1/auth/login", &observedBody{Reader: reader, started: started})
	go func() { defer close(done); handler.ServeHTTP(httptest.NewRecorder(), slow) }()
	t.Cleanup(func() { _ = writer.Close(); <-done })
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("login did not begin reading its body")
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"invalid","password":"unchanged"}`)))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("slow body blocked completed login: %d %s", response.Code, response.Body.String())
	}
	// Completed bodies still respect the shared password-work budget.
	api.loginSlots <- struct{}{}
	defer func() { <-api.loginSlots }()
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(`{"email":"invalid","password":"unchanged"}`)))
	if response.Code != http.StatusTooManyRequests {
		t.Fatalf("password concurrency limit lost: %d", response.Code)
	}
}

func TestRequestLogsIncludeStatusAndUnexpectedCause(t *testing.T) {
	for _, test := range []struct {
		name   string
		status int
		panic  bool
	}{
		{"unexpected error", 500, false}, {"panic", 500, true}, {"success", 204, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			var logs bytes.Buffer
			api := &API{logger: slog.New(slog.NewJSONHandler(&logs, nil))}
			handler := api.requestLog(api.recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if test.panic {
					panic("fixture failure")
				}
				if test.status == 500 {
					writeDomainError(w, errors.New("fixture failure"))
					return
				}
				w.WriteHeader(test.status)
			})))
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest("POST", "/api/v1/example?token=must-not-log", strings.NewReader("password=must-not-log")))
			if response.Code != test.status || strings.Contains(response.Body.String(), "fixture failure") || strings.Contains(logs.String(), "must-not-log") {
				t.Fatalf("response/log privacy contract: %d %s %s", response.Code, response.Body.String(), logs.String())
			}
			decoder := json.NewDecoder(&logs)
			var sawRequest, sawCause bool
			for decoder.More() {
				var record map[string]any
				if err := decoder.Decode(&record); err != nil {
					t.Fatal(err)
				}
				if record["request_id"] != response.Header().Get("X-Request-ID") || record["request_id"] == "" {
					t.Fatalf("missing correlated request ID: %+v", record)
				}
				if record["msg"] == "http request" {
					sawRequest = record["status"] == float64(test.status)
				}
				if record["error"] == "fixture failure" {
					sawCause = true
				}
			}
			if !sawRequest || (test.status == 500 && !sawCause) {
				t.Fatalf("incomplete request log: request=%t cause=%t", sawRequest, sawCause)
			}
		})
	}
}

func TestAPIPathPrefixValidation(t *testing.T) {
	for _, prefix := range []string{"", "/backend", "/app/v1-._~"} {
		if err := validateAPIPathPrefix(prefix); err != nil {
			t.Errorf("valid %q: %v", prefix, err)
		}
	}
	for _, prefix := range []string{"/", "backend", "/backend/", "//backend", "/a/../b", "/a/./b", "/a?x", "/a#x", "/a%2Fb", "https://example.test", "/a;", "/a\n"} {
		if err := validateAPIPathPrefix(prefix); err == nil {
			t.Errorf("accepted %q", prefix)
		}
	}
}
