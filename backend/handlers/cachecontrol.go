package handlers

import (
	"net/http"
	"regexp"

	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/opengittr/ogtr/backend/visitor"
)

// qrPathRe matches GET /api/v1/links/{id}/qr.
var qrPathRe = regexp.MustCompile(`^/api/v1/links/\d+/qr$`)

const qrCacheControl = "public, max-age=31536000, immutable"

// CacheControl sets caching headers gofr's typed responses cannot carry:
//
//   - GET /{code}: Cache-Control: no-store — a cached redirect (or cached
//     404) silently kills click tracking (ARCHITECTURE.md §4).
//   - GET /api/v1/links/{id}/qr: immutable on success — the QR image is a
//     deterministic function of the short URL. Error responses stay uncached.
func CacheControl() gofrHTTP.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Method == http.MethodGet {
				switch {
				case visitor.IsRedirectPath(r.URL.Path), visitor.IsPreviewPath(r.URL.Path):
					w.Header().Set("Cache-Control", "no-store")
				case qrPathRe.MatchString(r.URL.Path):
					w = &okCacheWriter{ResponseWriter: w, value: qrCacheControl}
				}
			}

			next.ServeHTTP(w, r)
		})
	}
}

// okCacheWriter adds a Cache-Control header only when the response is 200.
type okCacheWriter struct {
	http.ResponseWriter
	value string
}

func (w *okCacheWriter) WriteHeader(status int) {
	if status == http.StatusOK {
		w.Header().Set("Cache-Control", w.value)
	}

	w.ResponseWriter.WriteHeader(status)
}
