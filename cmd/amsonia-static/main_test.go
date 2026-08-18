package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticHandlerServesCleanRoutesAndRealNotFound(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "index.html"), "home")
	writeTestFile(t, filepath.Join(root, "core", "index.html"), "core")
	writeTestFile(t, filepath.Join(root, "404.html"), "missing specimen")

	handler := newStaticHandler(root, false)
	t.Cleanup(func() { _ = handler.Close() })

	tests := []struct {
		path       string
		statusCode int
		body       string
	}{
		{path: "/", statusCode: http.StatusOK, body: "home"},
		{path: "/core", statusCode: http.StatusOK, body: "core"},
		{path: "/core/", statusCode: http.StatusOK, body: "core"},
		{path: "/api/v1/tenants", statusCode: http.StatusNotFound, body: "missing specimen"},
		{path: "/sitemap.xml", statusCode: http.StatusNotFound, body: "missing specimen"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, test.path, nil)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != test.statusCode {
				t.Fatalf("status = %d, want %d", response.Code, test.statusCode)
			}
			if !strings.Contains(response.Body.String(), test.body) {
				t.Fatalf("body = %q, want substring %q", response.Body.String(), test.body)
			}
		})
	}
}

func TestStaticHandlerSPAFallbackExcludesMissingFiles(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "index.html"), "console")
	writeTestFile(t, filepath.Join(root, "robots.txt"), "User-agent: *\nAllow: /\n")

	handler := newStaticHandler(root, true)
	t.Cleanup(func() { _ = handler.Close() })

	login := httptest.NewRecorder()
	handler.ServeHTTP(login, httptest.NewRequest(http.MethodGet, "/login", nil))
	if login.Code != http.StatusOK || !strings.Contains(login.Body.String(), "console") {
		t.Fatalf("SPA route returned status %d and body %q", login.Code, login.Body.String())
	}

	sitemap := httptest.NewRecorder()
	handler.ServeHTTP(sitemap, httptest.NewRequest(http.MethodGet, "/sitemap.xml", nil))
	if sitemap.Code != http.StatusNotFound {
		t.Fatalf("missing sitemap status = %d, want %d", sitemap.Code, http.StatusNotFound)
	}

	robots := httptest.NewRecorder()
	handler.ServeHTTP(robots, httptest.NewRequest(http.MethodGet, "/robots.txt", nil))
	if robots.Code != http.StatusOK || !strings.Contains(robots.Body.String(), "Allow: /") {
		t.Fatalf("robots response = %d %q", robots.Code, robots.Body.String())
	}
}

func TestStaticHandlerHealthAndMethodGuard(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "index.html"), "home")
	handler := newStaticHandler(root, false)
	t.Cleanup(func() { _ = handler.Close() })

	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK || health.Body.String() != "ok\n" {
		t.Fatalf("health response = %d %q", health.Code, health.Body.String())
	}

	post := httptest.NewRecorder()
	handler.ServeHTTP(post, httptest.NewRequest(http.MethodPost, "/", nil))
	if post.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want %d", post.Code, http.StatusMethodNotAllowed)
	}
}

func TestStaticHandlerRejectsTraversalAndEscapingSymlinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	writeTestFile(t, filepath.Join(root, "404.html"), "not found")
	writeTestFile(t, filepath.Join(outside, "secret.html"), "secret")
	if err := os.Symlink(filepath.Join(outside, "secret.html"), filepath.Join(root, "escape.html")); err != nil {
		t.Fatal(err)
	}

	handler := newStaticHandler(root, false)
	t.Cleanup(func() { _ = handler.Close() })

	for _, requestPath := range []string{"/../secret.html", "/%2e%2e/secret.html", "/escape.html"} {
		t.Run(requestPath, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, requestPath, nil))
			if response.Code != http.StatusNotFound {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusNotFound)
			}
			if strings.Contains(response.Body.String(), "secret") {
				t.Fatal("response exposed a file outside the static root")
			}
		})
	}

	for _, requestPath := range []string{"relative.html", `/..\secret.html`, "/safe/../secret.html", "/safe/./file.html", "/bad\x00path"} {
		if _, ok := cleanRequestPath(requestPath); ok {
			t.Fatalf("cleanRequestPath(%q) accepted an unsafe path", requestPath)
		}
	}
}

func writeTestFile(t *testing.T, fileName, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(fileName), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fileName, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
