package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newWebRiskServer(t *testing.T, handler http.HandlerFunc) *WebRisk {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	return NewWebRisk("test-key", server.URL, server.Client(), nil)
}

func TestWebRisk_Verdicts(t *testing.T) {
	tests := []struct {
		desc         string
		body         string
		wantAllowed  bool
		wantCategory string
	}{
		{desc: "clean URL (empty object)", body: `{}`, wantAllowed: true},
		{
			desc:         "malware",
			body:         `{"threat":{"threatTypes":["MALWARE"],"expireTime":"2026-01-01T00:00:00Z"}}`,
			wantCategory: CategoryMalware,
		},
		{
			desc:         "social engineering maps to phishing",
			body:         `{"threat":{"threatTypes":["SOCIAL_ENGINEERING"]}}`,
			wantCategory: CategoryPhishing,
		},
		{
			desc:         "unwanted software maps to abuse",
			body:         `{"threat":{"threatTypes":["UNWANTED_SOFTWARE"]}}`,
			wantCategory: CategoryAbuse,
		},
		{
			desc:         "malware wins over social engineering",
			body:         `{"threat":{"threatTypes":["SOCIAL_ENGINEERING","MALWARE"]}}`,
			wantCategory: CategoryMalware,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			var gotQuery string

			w := newWebRiskServer(t, func(rw http.ResponseWriter, r *http.Request) {
				gotQuery = r.URL.RawQuery
				_, _ = rw.Write([]byte(tc.body))
			})

			verdict, err := w.Scan(context.Background(), "https://checked.example/x")

			require.NoError(t, err)
			assert.Equal(t, tc.wantAllowed, verdict.Allowed)
			assert.Equal(t, tc.wantCategory, verdict.Category)

			// The lookup carries the key, the URL and all three threat types.
			assert.Contains(t, gotQuery, "key=test-key")
			assert.Contains(t, gotQuery, "uri=https%3A%2F%2Fchecked.example%2Fx")
			assert.Contains(t, gotQuery, "threatTypes=MALWARE")
			assert.Contains(t, gotQuery, "threatTypes=SOCIAL_ENGINEERING")
			assert.Contains(t, gotQuery, "threatTypes=UNWANTED_SOFTWARE")
		})
	}
}

func TestWebRisk_FailsOpen(t *testing.T) {
	tests := []struct {
		desc    string
		handler http.HandlerFunc
	}{
		{
			desc: "server error",
			handler: func(rw http.ResponseWriter, _ *http.Request) {
				rw.WriteHeader(http.StatusInternalServerError)
			},
		},
		{
			desc: "garbage body",
			handler: func(rw http.ResponseWriter, _ *http.Request) {
				_, _ = rw.Write([]byte("not json"))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			w := newWebRiskServer(t, tc.handler)

			verdict, err := w.Scan(context.Background(), "https://checked.example/x")

			require.NoError(t, err, "web risk never errors — it fails open")
			assert.True(t, verdict.Allowed)
		})
	}
}

func TestWebRisk_UnreachableFailsOpen(t *testing.T) {
	w := NewWebRisk("test-key", "http://127.0.0.1:1", nil, nil)

	verdict, err := w.Scan(context.Background(), "https://checked.example/x")

	require.NoError(t, err)
	assert.True(t, verdict.Allowed)
}
