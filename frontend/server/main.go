// Tiny SPA file server: serves ./static, falling back to index.html for any
// path that doesn't match a real file (client-side routing: /links, /api-keys,
// /analytics, ... must work on full-page load). Generic static-file serving
// images can't do this fallback (they 404 unknown paths) or set the required
// Cache-Control headers, and nginx is against house rules — hence ~30 lines
// of Go.
//
// Cache policy: Vite's content-hashed /assets/* are immutable; everything
// else (notably index.html) is no-cache so deploys take effect immediately.
package main

import (
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const root = "./static"

func main() {
	index, err := os.ReadFile(filepath.Join(root, "index.html"))
	if err != nil {
		log.Fatalf("index.html missing: %v", err)
	}

	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		p := filepath.Clean(strings.TrimPrefix(r.URL.Path, "/"))
		full := filepath.Join(root, p)
		if st, err := os.Stat(full); err == nil && !st.IsDir() && p != "." {
			if strings.HasPrefix(p, "assets/") {
				w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			} else {
				w.Header().Set("Cache-Control", "no-cache")
			}
			http.ServeFile(w, r, full)
			return
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write(index)
	})

	log.Println("spa server listening on :8000")
	log.Fatal(http.ListenAndServe(":8000", nil))
}
