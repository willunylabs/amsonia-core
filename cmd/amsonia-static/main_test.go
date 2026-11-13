package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestStaticHandlerServesCleanRoutesAndRealNotFound(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "index.html"), "home")
	writeTestFile(t, filepath.Join(root, "404.html"), "missing specimen")

	handler := newStaticHandler(root, false)
	t.Cleanup(func() { _ = handler.Close() })

	tests := []struct {
		path       string
		statusCode int
		body       string
	}{
		{path: "/", statusCode: http.StatusOK, body: "home"},
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

func TestStaticHandlerRedirectsRetiredCorePages(t *testing.T) {
	root := t.TempDir()
	writeTestFile(t, filepath.Join(root, "404.html"), "missing specimen")
	handler := newStaticHandler(root, false)
	t.Cleanup(func() { _ = handler.Close() })

	tests := []struct {
		path     string
		location string
	}{
		{path: "/core", location: "https://github.com/willunylabs/amsonia-core"},
		{path: "/core/docs/getting-started", location: "https://github.com/willunylabs/amsonia-core"},
		{path: "/releases", location: "https://github.com/willunylabs/amsonia-core/releases"},
	}

	for _, test := range tests {
		t.Run(test.path, func(t *testing.T) {
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, test.path, nil))
			if response.Code != http.StatusMovedPermanently {
				t.Fatalf("status = %d, want %d", response.Code, http.StatusMovedPermanently)
			}
			if location := response.Header().Get("Location"); location != test.location {
				t.Fatalf("Location = %q, want %q", location, test.location)
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

func TestDeploymentRouteProbesMatchStaticHandler(t *testing.T) {
	for _, command := range []string{"bash", "curl"} {
		if _, err := exec.LookPath(command); err != nil {
			t.Skipf("deployment probe test requires %s: %v", command, err)
		}
	}
	data, err := os.ReadFile("../../deploy/site/deploy-amsonia-site.sh")
	if err != nil {
		t.Fatal(err)
	}
	_, probes, ok := strings.Cut(string(data), "# Keep these probes aligned with the static server's public route contract.\n")
	if !ok {
		t.Fatal("deployment route probes are missing")
	}
	probes, _, ok = strings.Cut(probes, "\nsucceeded=1")
	if !ok {
		t.Fatal("deployment route probes have no end marker")
	}
	root := t.TempDir()
	for _, name := range []string{"index.html", "robots.txt", "sitemap.xml", "404.html"} {
		writeTestFile(t, filepath.Join(root, name), "fixture")
	}
	handler := newStaticHandler(root, false)
	defer handler.Close()
	for _, broken := range []string{"", "/core", "/api/example", "/"} {
		t.Run("broken="+broken, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if broken != "" && r.URL.Path == broken {
					if broken == "/" {
						w.WriteHeader(http.StatusServiceUnavailable)
					}
					return
				}
				handler.ServeHTTP(w, r)
			}))
			defer server.Close()
			command := exec.CommandContext(t.Context(), "bash", "-euo", "pipefail", "-c", strings.ReplaceAll(probes, "http://127.0.0.1:8084", server.URL))
			output, err := command.CombinedOutput()
			if (err != nil) != (broken != "") {
				t.Fatalf("deployment probes: broken=%q error=%v output=%s", broken, err, output)
			}
		})
	}
}

func TestStaticAssetCachingDistinguishesBundledAndPublicFiles(t *testing.T) {
	root := t.TempDir()
	for _, test := range []struct {
		name, cache string
	}{
		{"index.html", "public, max-age=0, must-revalidate"},
		{"og-amsonia.svg", "public, max-age=0, must-revalidate"},
		{"favicon.png", "public, max-age=0, must-revalidate"},
		{"styles.css", "public, max-age=0, must-revalidate"},
		{"robots.txt", "public, max-age=3600"},
		{"sitemap.xml", "public, max-age=3600"},
		{"_astro/index.D9cP3mZq.css", "public, max-age=31536000, immutable"},
		{"_astro/client.B7eW2nXk.js", "public, max-age=31536000, immutable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			writeTestFile(t, filepath.Join(root, test.name), "asset")
			handler := newStaticHandler(root, false)
			defer handler.Close()
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/"+test.name, nil))
			if response.Code != http.StatusOK || response.Header().Get("Cache-Control") != test.cache {
				t.Fatalf("%s: status=%d cache=%q; want %q", test.name, response.Code, response.Header().Get("Cache-Control"), test.cache)
			}
		})
	}
}
