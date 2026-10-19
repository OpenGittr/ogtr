package visitor

import (
	"context"
	"net/http"
	"regexp"

	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// redirectPathRe matches the public redirect routes: one root path segment
// in the short-code/alias charset, optionally behind a brand-prefix segment
// (/{prefix}/{code}, alphanumeric). Neither charset has dots, so gofr's own
// single-segment routes (/favicon.ico) and dotted well-known paths never
// match (ARCHITECTURE.md §3).
var redirectPathRe = regexp.MustCompile(`^(/[a-zA-Z0-9]+)?/[a-zA-Z0-9_-]+$`)

// previewPathRe matches the public preview routes GET /{code}+ and
// GET /{prefix}/{code}+ — the redirect shapes with a trailing literal plus.
var previewPathRe = regexp.MustCompile(`^(/[a-zA-Z0-9]+)?/[a-zA-Z0-9_-]+\+$`)

// IsRedirectPath reports whether the request path is a public
// GET /{code} or GET /{prefix}/{code} redirect route.
func IsRedirectPath(path string) bool {
	return redirectPathRe.MatchString(path)
}

// IsPreviewPath reports whether the request path is a public
// GET /{code}+ or GET /{prefix}/{code}+ preview route.
func IsPreviewPath(path string) bool {
	return previewPathRe.MatchString(path)
}

type envContextKey struct{}

// ContextWithEnv returns a context carrying the visitor environment.
func ContextWithEnv(ctx context.Context, env Env) context.Context {
	return context.WithValue(ctx, envContextKey{}, env)
}

// FromContext returns the Env stored by Middleware. Requests that never
// passed the middleware (unit tests, misc wiring) get a default direct /
// desktop environment so resolution still works.
func FromContext(ctx context.Context) Env {
	if env, ok := ctx.Value(envContextKey{}).(Env); ok {
		return env
	}

	return Build("", "", "", "")
}

// Middleware attaches the visitor environment to resolution requests
// (GET /{code} and GET /api/v1/resolve), the preview page (GET /{code}+ —
// host scoping + guess throttle), the public abuse-report endpoint
// (rate-limited by client IP) and the bare-root GET / (whose handler needs
// the request Host to keep the WEBSITE_URL bounce off custom domains). The
// gofr handler layer only exposes query/path/body — headers, the Host and
// the remote address must be captured here, before the request is wrapped.
func Middleware() gofrHTTP.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if isResolveRequest(r) || isRootRequest(r) || isReportRequest(r) {
				env := Build(r.UserAgent(), r.Referer(), r.Header.Get("X-Forwarded-For"), r.RemoteAddr)
				env.Host = r.Host
				r = r.WithContext(ContextWithEnv(r.Context(), env))
			}

			next.ServeHTTP(w, r)
		})
	}
}

func isResolveRequest(r *http.Request) bool {
	return r.Method == http.MethodGet &&
		(r.URL.Path == "/api/v1/resolve" || IsRedirectPath(r.URL.Path) || IsPreviewPath(r.URL.Path))
}

func isRootRequest(r *http.Request) bool {
	return r.Method == http.MethodGet && r.URL.Path == "/"
}

func isReportRequest(r *http.Request) bool {
	return r.Method == http.MethodPost && r.URL.Path == "/api/v1/report"
}
