// Command amsonia-static serves generated Amsonia website assets without
// directory listings or implicit SPA fallbacks unless explicitly enabled.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/signal"
	"path"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8084", "HTTP listen address")
	root := flag.String("root", "site/dist", "directory containing generated static files")
	spa := flag.Bool("spa", false, "fall back to index.html for extensionless routes")
	flag.Parse()

	server := &http.Server{
		Addr:              *addr,
		Handler:           newStaticHandler(*root, *spa),
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

func newStaticHandler(root string, spa bool) http.Handler {
	absoluteRoot, err := filepath.Abs(root)
	if err != nil {
		panic(fmt.Sprintf("resolve static root: %v", err))
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

		fileName, found := resolveAsset(absoluteRoot, r.URL.Path, spa)
		if !found {
			serveNotFound(w, r, absoluteRoot)
			return
		}

		serveAsset(w, r, fileName)
	})
}

func resolveAsset(root, requestPath string, spa bool) (string, bool) {
	cleanPath := path.Clean("/" + requestPath)
	relativePath := strings.TrimPrefix(cleanPath, "/")
	if relativePath == "." {
		relativePath = ""
	}

	candidates := make([]string, 0, 2)
	if relativePath == "" {
		candidates = append(candidates, "index.html")
	} else if path.Ext(relativePath) != "" {
		candidates = append(candidates, relativePath)
	} else {
		candidates = append(candidates, filepath.Join(relativePath, "index.html"), relativePath+".html")
	}

	for _, candidate := range candidates {
		fileName, ok := existingFile(root, candidate)
		if ok {
			return fileName, true
		}
	}

	if spa && path.Ext(relativePath) == "" {
		return existingFile(root, "index.html")
	}

	return "", false
}

func existingFile(root, relativePath string) (string, bool) {
	fileName := filepath.Clean(filepath.Join(root, relativePath))
	if fileName != root && !strings.HasPrefix(fileName, root+string(filepath.Separator)) {
		return "", false
	}
	info, err := os.Stat(fileName)
	if err != nil || !info.Mode().IsRegular() {
		return "", false
	}
	return fileName, true
}

func serveAsset(w http.ResponseWriter, r *http.Request, fileName string) {
	extension := strings.ToLower(filepath.Ext(fileName))
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
	http.ServeFile(w, r, fileName)
}

func serveNotFound(w http.ResponseWriter, r *http.Request, root string) {
	notFoundFile, ok := existingFile(root, "404.html")
	if !ok {
		http.NotFound(w, r)
		return
	}

	file, err := os.Open(notFoundFile)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}

	w.Header().Set("Cache-Control", "public, max-age=0, must-revalidate")
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusNotFound)
	if r.Method == http.MethodGet {
		_, _ = io.Copy(w, io.NewSectionReader(file, 0, info.Size()))
	}
}
