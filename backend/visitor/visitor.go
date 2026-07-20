// Package visitor derives the click-tracking environment (device type, mobile
// OS, browser, referrer, IP) from an incoming HTTP request. Parsing is
// best-effort by design: unknown inputs degrade to the catch-all buckets and
// never error, so click recording can never block a redirect
// (ARCHITECTURE.md §4 "Resolution & redirect").
package visitor

import (
	"net"
	"net/url"
	"strings"
)

// Device types (FEATURES.md §4).
const (
	DeviceMobile  = "Mobile"
	DeviceTablet  = "Tablet"
	DeviceDesktop = "Desktop"
)

// Mobile OS buckets (FEATURES.md §4). OSNotApplicable is used for desktops.
const (
	OSIOS           = "iOS"
	OSAndroid       = "Android"
	OSWindows       = "Windows"
	OSOther         = "Other"
	OSNotApplicable = "NA"
)

// Browser buckets (FEATURES.md §4).
const (
	BrowserChrome  = "Chrome"
	BrowserSafari  = "Safari"
	BrowserFirefox = "Firefox"
	BrowserOpera   = "Opera"
	BrowserIE      = "IE"
	BrowserOther   = "Other"
)

// DirectReferrer is recorded when the request carries no Referer header.
const DirectReferrer = "Direct"

// Env is the visitor environment attached to a resolution request. Host is
// the raw request Host header (middleware-captured like the other fields —
// gofr handlers cannot read headers); it drives per-org custom-domain
// scoping. Empty means "no host known" and behaves as the deployment's own
// short domain.
type Env struct {
	DeviceType string
	MobileOS   string
	Browser    string
	Referrer   string
	IP         string
	Host       string
}

// Build derives an Env from raw request values. It never fails; unknown
// values fall into the catch-all buckets.
func Build(userAgent, referrer, forwardedFor, remoteAddr string) Env {
	device := deviceType(userAgent)

	if referrer == "" {
		referrer = DirectReferrer
	}

	return Env{
		DeviceType: device,
		MobileOS:   mobileOS(userAgent, device),
		Browser:    browser(userAgent),
		Referrer:   referrer,
		IP:         clientIP(forwardedFor, remoteAddr),
	}
}

// ReferrerHost returns the bare host of the referrer (without a "www."
// prefix); empty for direct traffic or unparseable referrers.
func (e Env) ReferrerHost() string {
	if e.Referrer == "" || e.Referrer == DirectReferrer {
		return ""
	}

	u, err := url.Parse(e.Referrer)
	if err != nil || u.Host == "" {
		return ""
	}

	return strings.TrimPrefix(strings.ToLower(u.Hostname()), "www.")
}

// deviceType buckets a User-Agent into Mobile / Tablet / Desktop.
func deviceType(ua string) string {
	switch {
	case strings.Contains(ua, "iPad") || strings.Contains(ua, "Tablet"),
		strings.Contains(ua, "Android") && !strings.Contains(ua, "Mobile"):
		return DeviceTablet
	case strings.Contains(ua, "Mobi") || strings.Contains(ua, "Android") ||
		strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPod"):
		return DeviceMobile
	default:
		return DeviceDesktop
	}
}

// mobileOS buckets a User-Agent into iOS / Android / Windows / Other; NA for
// desktops.
func mobileOS(ua, device string) string {
	if device == DeviceDesktop {
		return OSNotApplicable
	}

	switch {
	// Windows Phone first: its UAs carry an "Android" compatibility token.
	case strings.Contains(ua, "Windows"):
		return OSWindows
	case strings.Contains(ua, "iPhone") || strings.Contains(ua, "iPad") || strings.Contains(ua, "iPod"):
		return OSIOS
	case strings.Contains(ua, "Android"):
		return OSAndroid
	default:
		return OSOther
	}
}

// browser buckets a User-Agent into the fixed browser set. Order matters:
// Opera and Edge UAs also contain "Chrome", and Chrome UAs contain "Safari".
func browser(ua string) string {
	switch {
	case strings.Contains(ua, "OPR/") || strings.Contains(ua, "Opera"):
		return BrowserOpera
	case strings.Contains(ua, "Edg"): // Edge is outside the fixed set
		return BrowserOther
	case strings.Contains(ua, "MSIE") || strings.Contains(ua, "Trident"):
		return BrowserIE
	case strings.Contains(ua, "Firefox") || strings.Contains(ua, "FxiOS"):
		return BrowserFirefox
	case strings.Contains(ua, "Chrome") || strings.Contains(ua, "CriOS"):
		return BrowserChrome
	case strings.Contains(ua, "Safari"):
		return BrowserSafari
	default:
		return BrowserOther
	}
}

// clientIP returns the first valid IP in the X-Forwarded-For chain, falling
// back to the remote address (with or without a port).
func clientIP(forwardedFor, remoteAddr string) string {
	for hop := range strings.SplitSeq(forwardedFor, ",") {
		hop = strings.TrimSpace(hop)
		if hop != "" && net.ParseIP(hop) != nil {
			return hop
		}
	}

	if host, _, err := net.SplitHostPort(remoteAddr); err == nil && net.ParseIP(host) != nil {
		return host
	}

	if net.ParseIP(remoteAddr) != nil {
		return remoteAddr
	}

	return ""
}
