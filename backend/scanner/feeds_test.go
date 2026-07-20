package scanner

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fixtureFeed serves a switchable plaintext feed body — tests never fetch
// real feeds.
type fixtureFeed struct {
	body   atomic.Value // string
	fail   atomic.Bool
	server *httptest.Server
}

func newFixtureFeed(t *testing.T, body string) *fixtureFeed {
	t.Helper()

	f := &fixtureFeed{}
	f.body.Store(body)

	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if f.fail.Load() {
			w.WriteHeader(http.StatusInternalServerError)

			return
		}

		_, _ = w.Write([]byte(f.body.Load().(string)))
	}))
	t.Cleanup(f.server.Close)

	return f
}

func TestFeedList_ParseAndMatch(t *testing.T) {
	feed := newFixtureFeed(t, `
# comment line
// another comment

badhost.example
sub.listed.example.
https://exact.example/malware/payload.exe
https://exact.example/path/?q=1#frag
http://TRAILING.example/dir/
not a host line
`)

	feeds := NewFeedList([]string{feed.server.URL + "/phishfeed.txt"}, feed.server.Client(), nil)
	feeds.Refresh(context.Background())

	tests := []struct {
		desc        string
		url         string
		wantAllowed bool
	}{
		{desc: "host entry", url: "https://badhost.example/anything?x=1", wantAllowed: false},
		{desc: "subdomain of host entry", url: "https://deep.badhost.example/", wantAllowed: false},
		{desc: "host entry with trailing dot in feed", url: "http://sub.listed.example/x", wantAllowed: false},
		{desc: "parent of a listed subdomain stays clean", url: "https://listed.example/", wantAllowed: true},
		{desc: "exact URL entry", url: "https://exact.example/malware/payload.exe", wantAllowed: false},
		{desc: "exact URL, fragment ignored", url: "https://exact.example/path/?q=1", wantAllowed: false},
		{desc: "exact URL with trailing slash normalized", url: "http://trailing.example/dir", wantAllowed: false},
		{desc: "same host different path stays clean", url: "https://exact.example/other", wantAllowed: true},
		{desc: "unlisted host", url: "https://clean.example/", wantAllowed: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			verdict, err := feeds.Scan(context.Background(), tc.url)

			require.NoError(t, err)
			assert.Equal(t, tc.wantAllowed, verdict.Allowed)

			if !tc.wantAllowed {
				// Feed URL contains "phish" → phishing category.
				assert.Equal(t, CategoryPhishing, verdict.Category)
			}
		})
	}
}

func TestFeedList_RefreshReplacesAndKeepsLastGoodOnFailure(t *testing.T) {
	ctx := context.Background()
	feed := newFixtureFeed(t, "first.example\n")

	feeds := NewFeedList([]string{feed.server.URL + "/urlhaus.txt"}, feed.server.Client(), nil)
	feeds.Refresh(ctx)

	verdict, _ := feeds.Scan(ctx, "https://first.example/x")
	require.False(t, verdict.Allowed)
	assert.Equal(t, CategoryMalware, verdict.Category, "urlhaus-named feed maps to malware")

	// A successful refresh replaces the data.
	feed.body.Store("second.example\n")
	feeds.Refresh(ctx)

	verdict, _ = feeds.Scan(ctx, "https://first.example/x")
	assert.True(t, verdict.Allowed, "old entry gone after successful refresh")
	verdict, _ = feeds.Scan(ctx, "https://second.example/x")
	assert.False(t, verdict.Allowed)

	// A failing refresh keeps the last-good data.
	feed.fail.Store(true)
	feeds.Refresh(ctx)

	verdict, _ = feeds.Scan(ctx, "https://second.example/x")
	assert.False(t, verdict.Allowed, "last-good data survives a failed refresh")
}

func TestFeedList_EmptyBeforeFirstLoadAllowsEverything(t *testing.T) {
	feeds := NewFeedList([]string{"http://never-fetched.invalid/feed"}, nil, nil)

	verdict, err := feeds.Scan(context.Background(), "https://anything.example/")

	require.NoError(t, err)
	assert.True(t, verdict.Allowed)
}

func TestFeedList_UnreachableFeedNeverErrors(t *testing.T) {
	// Refresh against a dead endpoint: logs (nil logger here) and keeps
	// going — mirrors "feed fetch failures never block startup".
	feeds := NewFeedList([]string{"http://127.0.0.1:1/feed"}, nil, nil)
	feeds.Refresh(context.Background())

	verdict, err := feeds.Scan(context.Background(), "https://anything.example/")

	require.NoError(t, err)
	assert.True(t, verdict.Allowed)
}

func TestFeedCategory(t *testing.T) {
	assert.Equal(t, CategoryPhishing, feedCategory("https://openphish.example/feed.txt"))
	assert.Equal(t, CategoryMalware, feedCategory("https://urlhaus.example/text/"))
	assert.Equal(t, CategoryMalware, feedCategory("https://lists.example/malware-hosts.txt"))
	assert.Equal(t, CategoryAbuse, feedCategory("https://coinblocker.example/list.txt"))
}
