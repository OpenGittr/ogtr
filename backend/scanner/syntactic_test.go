package scanner

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyntactic_Scan(t *testing.T) {
	s := NewSyntactic([]string{"corp.sho.rt", " Spaced.Example "})

	tests := []struct {
		desc         string
		url          string
		wantAllowed  bool
		wantCategory string
	}{
		// Clean URLs.
		{desc: "plain https", url: "https://example.com/page", wantAllowed: true},
		{desc: "plain http", url: "http://example.com", wantAllowed: true},
		{desc: "query and fragment", url: "https://example.com/p?x=1#frag", wantAllowed: true},
		{desc: "punycode without mixed scripts", url: "https://xn--mnchen-3ya.de/page", wantAllowed: true},
		{desc: "all-cyrillic label is fine (single script)", url: "https://пример.example", wantAllowed: true},

		// Scheme guard.
		{desc: "javascript scheme", url: "javascript:alert(1)", wantCategory: CategoryPolicy},
		{desc: "ftp scheme", url: "ftp://example.com/file", wantCategory: CategoryPolicy},
		{desc: "data scheme", url: "data:text/html,hi", wantCategory: CategoryPolicy},
		{desc: "schemeless", url: "example.com/page", wantCategory: CategoryPolicy},

		// Userinfo guard.
		{desc: "userinfo trick", url: "https://google.com@evil.example/login", wantCategory: CategoryPolicy},
		{desc: "user:pass", url: "https://a:b@example.com", wantCategory: CategoryPolicy},

		// IP literals — public and internal ranges alike.
		{desc: "public IPv4", url: "https://8.8.8.8/x", wantCategory: CategoryPolicy},
		{desc: "private 10.x", url: "http://10.0.0.5/admin", wantCategory: CategoryPolicy},
		{desc: "private 192.168.x", url: "http://192.168.1.1", wantCategory: CategoryPolicy},
		{desc: "private 172.16.x", url: "http://172.16.0.9", wantCategory: CategoryPolicy},
		{desc: "loopback", url: "http://127.0.0.1:8080/", wantCategory: CategoryPolicy},
		{desc: "link-local", url: "http://169.254.169.254/latest/meta-data", wantCategory: CategoryPolicy},
		{desc: "IPv6 loopback", url: "http://[::1]/", wantCategory: CategoryPolicy},
		{desc: "IPv6 global", url: "http://[2001:db8::1]/", wantCategory: CategoryPolicy},

		// Loopback by name.
		{desc: "localhost", url: "http://localhost:3000/x", wantCategory: CategoryPolicy},
		{desc: "localhost subdomain", url: "http://app.localhost/x", wantCategory: CategoryPolicy},

		// Missing host.
		{desc: "empty host", url: "https:///path", wantCategory: CategoryPolicy},

		// Shortener chaining — built-ins, subdomains, config additions.
		{desc: "bit.ly", url: "https://bit.ly/abc", wantCategory: CategoryPolicy},
		{desc: "tinyurl", url: "https://tinyurl.com/abc", wantCategory: CategoryPolicy},
		{desc: "t.co", url: "https://t.co/xyz", wantCategory: CategoryPolicy},
		{desc: "goo.gl", url: "https://goo.gl/xyz", wantCategory: CategoryPolicy},
		{desc: "is.gd", url: "https://is.gd/xyz", wantCategory: CategoryPolicy},
		{desc: "buff.ly", url: "https://buff.ly/xyz", wantCategory: CategoryPolicy},
		{desc: "ow.ly", url: "https://ow.ly/xyz", wantCategory: CategoryPolicy},
		{desc: "shortener subdomain", url: "https://www.tinyurl.com/abc", wantCategory: CategoryPolicy},
		{desc: "shortener case-insensitive", url: "https://BIT.LY/abc", wantCategory: CategoryPolicy},
		{desc: "configured extra shortener", url: "https://corp.sho.rt/abc", wantCategory: CategoryPolicy},
		{desc: "configured extra is trimmed+lowered", url: "https://spaced.example/abc", wantCategory: CategoryPolicy},
		{desc: "shortener as substring of another host allowed", url: "https://notbit.ly.example.com/x", wantAllowed: true},

		// Homograph (mixed-script) labels.
		{desc: "latin+cyrillic label", url: "https://gооgle.com/login", wantCategory: CategoryPhishing},
		{desc: "latin+greek label", url: "https://pαypal.example/x", wantCategory: CategoryPhishing},
		{desc: "punycode-encoded homograph", url: "https://xn--ggle-55da.com/x", wantCategory: CategoryPhishing},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			verdict, err := s.Scan(context.Background(), tc.url)

			require.NoError(t, err, "syntactic layer never errors")
			assert.Equal(t, tc.wantAllowed, verdict.Allowed)
			assert.Equal(t, tc.wantCategory, verdict.Category)
		})
	}
}

func TestPipeline_FirstFlagWinsAndErrorsSkip(t *testing.T) {
	ctx := context.Background()

	failing := scanFunc(func(context.Context, string) (Verdict, error) {
		return Verdict{}, assert.AnError
	})
	flagging := scanFunc(func(context.Context, string) (Verdict, error) {
		return Flag(CategoryMalware), nil
	})
	allowing := scanFunc(func(context.Context, string) (Verdict, error) {
		return Allow(), nil
	})

	// An erroring layer is skipped (fail-open) and later layers still run.
	verdict, err := NewPipeline(nil, failing, flagging, allowing).Scan(ctx, "https://x.example")
	require.NoError(t, err)
	assert.False(t, verdict.Allowed)
	assert.Equal(t, CategoryMalware, verdict.Category)

	// All-allow pipeline allows; empty pipeline allows.
	verdict, err = NewPipeline(nil, allowing).Scan(ctx, "https://x.example")
	require.NoError(t, err)
	assert.True(t, verdict.Allowed)

	verdict, err = NewPipeline(nil).Scan(ctx, "https://x.example")
	require.NoError(t, err)
	assert.True(t, verdict.Allowed)
}

// scanFunc adapts a function to Scanner for pipeline tests.
type scanFunc func(ctx context.Context, rawURL string) (Verdict, error)

func (f scanFunc) Scan(ctx context.Context, rawURL string) (Verdict, error) { return f(ctx, rawURL) }
