package scanner

import (
	"bufio"
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// feedFetchTimeout bounds one feed download; a slow feed mirror must never
// hang a refresh (and startup loads run in the background anyway).
const feedFetchTimeout = 30 * time.Second

// maxFeedLine skips absurdly long lines (corrupt feeds) instead of storing
// them.
const maxFeedLine = 4096

// feedData is the parsed content of one feed: bare-host lines land in
// hosts, full-URL lines in exact (normalized, fragment stripped).
type feedData struct {
	hosts map[string]struct{}
	exact map[string]struct{}
}

// FeedList is the feed-based blocklist layer (BLOCKLIST_FEED_URLS): each
// feed is a plaintext document with one host or URL per line (# comments
// allowed) — the format URLhaus, OpenPhish and CoinBlocker publish.
//
// Feeds are loaded in the background at startup and refreshed on an
// interval (BLOCKLIST_REFRESH_INTERVAL, gofr cron). A failed fetch logs and
// keeps that feed's last-good data — a feed outage never blocks startup and
// never empties the list.
type FeedList struct {
	feedURLs []string
	client   *http.Client
	log      Logger

	mu    sync.RWMutex
	feeds map[string]feedData // by feed URL; entries survive failed refreshes
}

// NewFeedList builds the layer over the configured feed URLs. client may be
// nil (a default with a fetch timeout is used); tests inject their own
// against httptest fixture feeds — real feeds are never fetched in tests.
func NewFeedList(feedURLs []string, client *http.Client, log Logger) *FeedList {
	if client == nil {
		client = &http.Client{Timeout: feedFetchTimeout}
	}

	return &FeedList{
		feedURLs: feedURLs,
		client:   client,
		log:      log,
		feeds:    make(map[string]feedData, len(feedURLs)),
	}
}

// Refresh fetches every configured feed. Per-feed failures are logged and
// keep that feed's last-good data; Refresh itself never fails.
func (f *FeedList) Refresh(ctx context.Context) {
	for _, feedURL := range f.feedURLs {
		data, err := f.fetch(ctx, feedURL)
		if err != nil {
			if f.log != nil {
				f.log.Errorf("blocklist feed %s refresh failed (keeping last-good data): %v", feedURL, err)
			}

			continue
		}

		f.mu.Lock()
		f.feeds[feedURL] = data
		f.mu.Unlock()

		if f.log != nil {
			f.log.Infof("blocklist feed %s loaded: %d hosts, %d urls", feedURL, len(data.hosts), len(data.exact))
		}
	}
}

// fetch downloads and parses one feed.
func (f *FeedList) fetch(ctx context.Context, feedURL string) (feedData, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, feedURL, http.NoBody)
	if err != nil {
		return feedData{}, err
	}

	resp, err := f.client.Do(req)
	if err != nil {
		return feedData{}, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return feedData{}, fmt.Errorf("feed answered HTTP %d", resp.StatusCode)
	}

	data := feedData{hosts: map[string]struct{}{}, exact: map[string]struct{}{}}

	sc := bufio.NewScanner(resp.Body)
	sc.Buffer(make([]byte, maxFeedLine), maxFeedLine)

	for sc.Scan() {
		parseFeedLine(sc.Text(), data)
	}

	// A scanner error (e.g. an over-long line) truncates the feed; keep what
	// parsed — partial data still protects.
	if err := sc.Err(); err != nil && f.log != nil {
		f.log.Warnf("blocklist feed %s parsed partially: %v", feedURL, err)
	}

	return data, nil
}

// parseFeedLine classifies one feed line: blank/comment lines are skipped,
// lines with a scheme are exact-URL entries, everything else is a bare host.
func parseFeedLine(line string, data feedData) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") {
		return
	}

	if strings.Contains(line, "://") {
		if normalized, ok := normalizeExactURL(line); ok {
			data.exact[normalized] = struct{}{}
		}

		return
	}

	// Bare host line (CoinBlocker-style). Tolerate "host/path"-less junk by
	// keeping only syntactically host-shaped values.
	host := strings.ToLower(strings.TrimSuffix(line, "."))
	if host != "" && !strings.ContainsAny(host, " /:?#@") {
		data.hosts[host] = struct{}{}
	}
}

// normalizeExactURL canonicalizes a URL for exact matching: lowercased
// scheme+host, fragment stripped, query kept, trailing slash on a bare path
// removed.
func normalizeExactURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" {
		return "", false
	}

	u.Fragment = ""
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(u.Host)

	s := u.String()
	if strings.HasSuffix(u.Path, "/") && u.RawQuery == "" {
		s = strings.TrimSuffix(s, "/")
	}

	return s, true
}

// Scan implements Scanner: the URL is flagged when its host (or a parent
// domain of it) is on a host feed, or the URL itself is on a URL feed. The
// category is derived from the feed's own URL (phish→phishing, malware/
// urlhaus→malware, everything else→abuse) — coarse by design.
func (f *FeedList) Scan(_ context.Context, rawURL string) (Verdict, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Allow(), nil // structural problems are the syntactic layer's job
	}

	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")

	normalized, _ := normalizeExactURL(rawURL)

	f.mu.RLock()
	defer f.mu.RUnlock()

	for feedURL, data := range f.feeds {
		if hostOnList(host, data.hosts) {
			return Flag(feedCategory(feedURL)), nil
		}

		if normalized != "" {
			if _, ok := data.exact[normalized]; ok {
				return Flag(feedCategory(feedURL)), nil
			}
		}
	}

	return Allow(), nil
}

// hostOnList matches the host and each parent domain against the set, so a
// feed entry "evil.example" also covers "cdn.evil.example".
func hostOnList(host string, set map[string]struct{}) bool {
	for host != "" {
		if _, ok := set[host]; ok {
			return true
		}

		dot := strings.IndexByte(host, '.')
		if dot < 0 {
			return false
		}

		host = host[dot+1:]
	}

	return false
}

// feedCategory maps a feed to a coarse category by its URL: OpenPhish-style
// feeds report phishing, URLhaus-style feeds malware, anything else abuse.
func feedCategory(feedURL string) string {
	lower := strings.ToLower(feedURL)

	switch {
	case strings.Contains(lower, "phish"):
		return CategoryPhishing
	case strings.Contains(lower, "urlhaus"), strings.Contains(lower, "malware"):
		return CategoryMalware
	default:
		return CategoryAbuse
	}
}
