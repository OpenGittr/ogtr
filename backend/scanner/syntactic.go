package scanner

import (
	"context"
	"net"
	"net/url"
	"strings"
	"unicode"

	"golang.org/x/net/idna"
)

// builtinShorteners are well-known public URL shorteners. Shortening a link
// that itself points at a shortener (chaining) hides the real destination
// from every later scan, so it is refused outright. SHORTENER_DOMAINS config
// appends deployment-specific additions.
var builtinShorteners = []string{
	"bit.ly", "tinyurl.com", "t.co", "goo.gl", "is.gd", "buff.ly", "ow.ly",
	"rebrand.ly", "cutt.ly", "shorturl.at", "rb.gy", "tiny.cc", "v.gd", "s.id",
}

// Syntactic is the always-on structural guard layer: it needs no
// configuration, no network and never errors — it is the floor of the
// pipeline. It rejects URL shapes that are abusive by construction:
//
//   - non-http(s) schemes (defense in depth behind creation validation)
//   - userinfo tricks (https://google.com@evil.example/)
//   - IP-literal hosts (all of them — public IPs hide the destination's
//     identity, and private/loopback/link-local ranges probe the
//     deployment's own network via anyone who clicks)
//   - localhost-style hostnames (loopback by name)
//   - mixed-script (homograph) hostname labels — basic lookalike detection
//   - known URL-shortener hosts (chaining, see builtinShorteners)
type Syntactic struct {
	shorteners map[string]struct{}
}

// NewSyntactic builds the guard; extraShorteners come from the
// SHORTENER_DOMAINS config (comma-separated, may be empty).
func NewSyntactic(extraShorteners []string) *Syntactic {
	set := make(map[string]struct{}, len(builtinShorteners)+len(extraShorteners))

	for _, d := range builtinShorteners {
		set[d] = struct{}{}
	}

	for _, d := range extraShorteners {
		if d = strings.ToLower(strings.TrimSpace(d)); d != "" {
			set[d] = struct{}{}
		}
	}

	return &Syntactic{shorteners: set}
}

// Scan implements Scanner. It never returns an error.
func (s *Syntactic) Scan(_ context.Context, rawURL string) (Verdict, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return Flag(CategoryPolicy), nil
	}

	scheme := strings.ToLower(u.Scheme)
	if scheme != "http" && scheme != "https" {
		return Flag(CategoryPolicy), nil
	}

	// https://trusted.example@evil.example/ — the part before @ is userinfo,
	// not the host; it exists in link destinations only to deceive.
	if u.User != nil {
		return Flag(CategoryPolicy), nil
	}

	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	if host == "" {
		return Flag(CategoryPolicy), nil
	}

	// IP literals: a public IP hides who the destination is; private,
	// loopback and link-local ranges turn every click into a probe of the
	// deployment's (or the visitor's) internal network.
	if ip := net.ParseIP(host); ip != nil {
		return Flag(CategoryPolicy), nil
	}

	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return Flag(CategoryPolicy), nil
	}

	if s.isShortener(host) {
		return Flag(CategoryPolicy), nil
	}

	if hasMixedScriptLabel(host) {
		return Flag(CategoryPhishing), nil
	}

	return Allow(), nil
}

// isShortener reports whether the host is (or is a subdomain of) a known
// URL shortener.
func (s *Syntactic) isShortener(host string) bool {
	for {
		if _, ok := s.shorteners[host]; ok {
			return true
		}

		dot := strings.IndexByte(host, '.')
		if dot < 0 {
			return false
		}

		host = host[dot+1:]
	}
}

// hasMixedScriptLabel is basic homograph detection: a hostname label whose
// letters mix Latin with Cyrillic or Greek (e.g. "gооgle" with Cyrillic о)
// exists to look like another name. Punycode labels (xn--…) are decoded
// first so encoded homographs are caught too.
func hasMixedScriptLabel(host string) bool {
	for label := range strings.SplitSeq(host, ".") {
		if strings.HasPrefix(label, "xn--") {
			if decoded, err := idna.ToUnicode(label); err == nil {
				label = decoded
			}
		}

		var latin, cyrillic, greek bool

		for _, r := range label {
			switch {
			case unicode.Is(unicode.Latin, r):
				latin = true
			case unicode.Is(unicode.Cyrillic, r):
				cyrillic = true
			case unicode.Is(unicode.Greek, r):
				greek = true
			}
		}

		if (latin && cyrillic) || (latin && greek) || (cyrillic && greek) {
			return true
		}
	}

	return false
}
