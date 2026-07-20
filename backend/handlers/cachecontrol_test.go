package handlers

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCacheControl(t *testing.T) {
	tests := []struct {
		desc       string
		method     string
		path       string
		status     int
		wantHeader string
	}{
		{
			desc: "redirect gets no-store", method: http.MethodGet, path: "/abc1234",
			status: http.StatusFound, wantHeader: "no-store",
		},
		{
			desc: "redirect 404 also gets no-store", method: http.MethodGet, path: "/nope",
			status: http.StatusNotFound, wantHeader: "no-store",
		},
		{
			desc: "qr 200 gets immutable", method: http.MethodGet, path: "/api/v1/links/9/qr",
			status: http.StatusOK, wantHeader: qrCacheControl,
		},
		{
			desc: "qr 404 stays uncached", method: http.MethodGet, path: "/api/v1/links/9/qr",
			status: http.StatusNotFound, wantHeader: "",
		},
		{
			desc: "other api paths untouched", method: http.MethodGet, path: "/api/v1/links",
			status: http.StatusOK, wantHeader: "",
		},
		{
			desc: "POST untouched", method: http.MethodPost, path: "/abc1234",
			status: http.StatusOK, wantHeader: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			})

			req := httptest.NewRequest(tc.method, tc.path, http.NoBody)
			rec := httptest.NewRecorder()

			CacheControl()(next).ServeHTTP(rec, req)

			assert.Equal(t, tc.status, rec.Code)
			assert.Equal(t, tc.wantHeader, rec.Header().Get("Cache-Control"))
		})
	}
}
