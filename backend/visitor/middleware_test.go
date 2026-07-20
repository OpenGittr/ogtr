package visitor

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsRedirectPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"/abc1234", true},
		{"/my-brand_1", true},
		{"/api", true}, // matches the pattern; "api" is a reserved word so it can never resolve
		{"/", false},
		{"/api/v1/links", false},
		{"/favicon.ico", false},
		{"/.well-known/health", false},
		{"/abc/def", false},
	}

	for _, tc := range tests {
		t.Run(tc.path, func(t *testing.T) {
			assert.Equal(t, tc.want, IsRedirectPath(tc.path))
		})
	}
}

func TestMiddleware(t *testing.T) {
	tests := []struct {
		desc    string
		method  string
		path    string
		wantEnv bool
	}{
		{"redirect path gets an env", http.MethodGet, "/abc1234", true},
		{"resolve path gets an env", http.MethodGet, "/api/v1/resolve?code=x", true},
		{"api path gets none", http.MethodGet, "/api/v1/links", false},
		{"POST gets none", http.MethodPost, "/abc1234", false},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			var (
				sawEnv bool
				env    Env
			)

			next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
				_, sawEnv = r.Context().Value(envContextKey{}).(Env)
				env = FromContext(r.Context())
			})

			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			req.Header.Set("User-Agent", uaIPhoneSafari)
			req.Header.Set("Referer", "https://t.co/x")
			req.RemoteAddr = "203.0.113.7:52814"

			Middleware()(next).ServeHTTP(httptest.NewRecorder(), req)

			assert.Equal(t, tc.wantEnv, sawEnv)

			if tc.wantEnv {
				assert.Equal(t, DeviceMobile, env.DeviceType)
				assert.Equal(t, "https://t.co/x", env.Referrer)
				assert.Equal(t, "203.0.113.7", env.IP)
			}
		})
	}
}

func TestFromContext_Default(t *testing.T) {
	env := FromContext(context.Background())

	assert.Equal(t, DeviceDesktop, env.DeviceType)
	assert.Equal(t, OSNotApplicable, env.MobileOS)
	assert.Equal(t, BrowserOther, env.Browser)
	assert.Equal(t, DirectReferrer, env.Referrer)
	assert.Empty(t, env.IP)
}
