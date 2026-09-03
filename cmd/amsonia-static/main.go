// Command amsonia-static serves generated Amsonia website assets without
// directory listings or implicit SPA fallbacks unless explicitly enabled.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path"
	"strings"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8084", "HTTP listen address")
	root := flag.String("root", "site/dist", "directory containing generated static files")
	spa := flag.Bool("spa", false, "fall back to index.html for extensionless routes")
	flag.Parse()

	handler := newStaticHandler(*root, *spa)
	defer handler.Close()

	server := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("static server shutdown: %v", err)
		}
	}()

	log.Printf("serving %s on %s (spa=%t)", *root, *addr, *spa)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

type staticHandler struct {
	root *os.Root
	spa  bool
}

type staticAsset struct {
	name string
	file *os.File
	info fs.FileInfo
}

func newStaticHandler(root string, spa bool) *staticHandler {
	safeRoot, err := os.OpenRoot(root)
	if err != nil {
		panic(fmt.Sprintf("open static root: %v", err))
	}
	return &staticHandler{root: safeRoot, spa: spa}
}

func (h *staticHandler) Close() error {
	return h.root.Close()
}

func (h *staticHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}

	if r.URL.Path == "/healthz" {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		if r.Method == http.MethodGet {
			_, _ = io.WriteString(w, "ok\n")
		}
		return
	}

	if target, ok := legacyExternalRedirect(r.URL.Path); ok {
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}

	asset, found := resolveAsset(h.root, r.URL.Path, h.spa)
	if !found {
		serveNotFound(w, r, h.root)
		return
	}
	defer asset.file.Close()

	serveAsset(w, r, asset)
}

func legacyExternalRedirect(requestPath string) (string, bool) {
	switch {
	case requestPath == "/releases" || requestPath == "/releases/":
		return "https://github.com/willunylabs/amsonia-core/releases", true
	case requestPath == "/core" || strings.HasPrefix(requestPath, "/core/"):
		return "https://github.com/willunylabs/amsonia-core", true
	default:
		return "", false
	}
}

func resolveAsset(root *os.Root, requestPath string, spa bool) (staticAsset, bool) {
	cleanPath, ok := cleanRequestPath(requestPath)
	if !ok {
		return staticAsset{}, false
	}
	relativePath := strings.TrimPrefix(cleanPath, "/")

	candidates := make([]string, 0, 2)
	if relativePath == "" {
		candidates = append(candidates, "index.html")
	} else if path.Ext(relativePath) != "" {
		candidates = append(candidates, relativePath)
	} else {
		candidates = append(candidates, path.Join(relativePath, "index.html"), relativePath+".html")
	}

	for _, candidate := range candidates {
		asset, ok := existingFile(root, candidate)
		if ok {
			return asset, true
		}
	}

	if spa && path.Ext(relativePath) == "" {
		return existingFile(root, "index.html")
	}

	return staticAsset{}, false
}

func cleanRequestPath(requestPath string) (string, bool) {
	if !strings.HasPrefix(requestPath, "/") || strings.ContainsRune(requestPath, '\x00') || strings.Contains(requestPath, `\`) {
		return "", false
	}
	for _, segment := range strings.Split(requestPath, "/") {
		if segment == "." || segment == ".." {
			return "", false
		}
	}

	cleanPath := path.Clean(requestPath)
	if cleanPath == "." {
		cleanPath = "/"
	}
	relativePath := strings.TrimPrefix(cleanPath, "/")
	if relativePath != "" && !fs.ValidPath(relativePath) {
		return "", false
	}
	return cleanPath, true
}

func existingFile(root *os.Root, relativePath string) (staticAsset, bool) {
	if !fs.ValidPath(relativePath) {
		return staticAsset{}, false
	}
	file, err := root.Open(relativePath)
	if err != nil {
		return staticAsset{}, false
	}
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		file.Close()
		return staticAsset{}, false
	}
	return staticAsset{name: relativePath, file: file, info: info}, true
}

func serveAsset(w http.ResponseWriter, r *http.Request, asset staticAsset) {
	extension := strings.ToLower(path.Ext(asset.name))
	if extension == ".html" {
		w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	} else if extension == ".xml" || extension == ".txt" {
		w.Header().Set("Cache-Control", "public, max-age=3600")
	} else {
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	}
	if contentType := mime.TypeByExtension(extension); contentType != "" {
		w.Header().Set("Content-Type", contentType)
	}
	http.ServeContent(w, r, asset.name, asset.info.ModTime(), asset.file)
}

func serveNotFound(w http.ResponseWriter, r *http.Request, root *os.Root) {
	asset, ok := existingFile(root, "404.html")
	if !ok {
		http.NotFound(w, r)
		return
	}
	defer asset.file.Close()

	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method == http.MethodGet {
		_, _ = io.Copy(w, io.NewSectionReader(asset.file, 0, asset.info.Size()))
	}
}
