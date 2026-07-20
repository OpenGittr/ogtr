package visitor

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const (
	uaIPhoneSafari = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 " +
		"(KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	uaIPhoneChrome = "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X) AppleWebKit/605.1.15 " +
		"(KHTML, like Gecko) CriOS/120.0.0.0 Mobile/15E148 Safari/604.1"
	uaAndroidChrome = "Mozilla/5.0 (Linux; Android 14; Pixel 8) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/120.0.0.0 Mobile Safari/537.36"
	uaAndroidTablet = "Mozilla/5.0 (Linux; Android 14; SM-X710) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	uaIPad = "Mozilla/5.0 (iPad; CPU OS 17_0 like Mac OS X) AppleWebKit/605.1.15 " +
		"(KHTML, like Gecko) Version/17.0 Mobile/15E148 Safari/604.1"
	uaMacChrome = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"
	uaMacSafari = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15 " +
		"(KHTML, like Gecko) Version/17.0 Safari/605.1.15"
	uaWinFirefox = "Mozilla/5.0 (Windows NT 10.0; Win64; x64; rv:120.0) Gecko/20100101 Firefox/120.0"
	uaWinEdge    = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 Edg/120.0.0.0"
	uaWinOpera = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 " +
		"(KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36 OPR/106.0.0.0"
	uaIE11         = "Mozilla/5.0 (Windows NT 10.0; Trident/7.0; rv:11.0) like Gecko"
	uaWindowsPhone = "Mozilla/5.0 (Windows Phone 10.0; Android 6.0.1; Microsoft; Lumia 950) " +
		"AppleWebKit/537.36 (KHTML, like Gecko) Chrome/52.0.2743.116 Mobile Safari/537.36 Edge/15.15254"
)

func TestBuild_UserAgents(t *testing.T) {
	tests := []struct {
		desc       string
		ua         string
		wantDevice string
		wantOS     string
		wantBrows  string
	}{
		{"iPhone Safari", uaIPhoneSafari, DeviceMobile, OSIOS, BrowserSafari},
		{"iPhone Chrome", uaIPhoneChrome, DeviceMobile, OSIOS, BrowserChrome},
		{"Android phone Chrome", uaAndroidChrome, DeviceMobile, OSAndroid, BrowserChrome},
		{"Android tablet", uaAndroidTablet, DeviceTablet, OSAndroid, BrowserChrome},
		{"iPad", uaIPad, DeviceTablet, OSIOS, BrowserSafari},
		{"Mac Chrome", uaMacChrome, DeviceDesktop, OSNotApplicable, BrowserChrome},
		{"Mac Safari", uaMacSafari, DeviceDesktop, OSNotApplicable, BrowserSafari},
		{"Windows Firefox", uaWinFirefox, DeviceDesktop, OSNotApplicable, BrowserFirefox},
		{"Edge is outside the fixed browser set", uaWinEdge, DeviceDesktop, OSNotApplicable, BrowserOther},
		{"Windows Opera", uaWinOpera, DeviceDesktop, OSNotApplicable, BrowserOpera},
		{"IE 11", uaIE11, DeviceDesktop, OSNotApplicable, BrowserIE},
		{"Windows Phone", uaWindowsPhone, DeviceMobile, OSWindows, BrowserOther},
		{"curl-ish empty UA", "", DeviceDesktop, OSNotApplicable, BrowserOther},
		{"curl", "curl/8.4.0", DeviceDesktop, OSNotApplicable, BrowserOther},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			env := Build(tc.ua, "", "", "")

			assert.Equal(t, tc.wantDevice, env.DeviceType)
			assert.Equal(t, tc.wantOS, env.MobileOS)
			assert.Equal(t, tc.wantBrows, env.Browser)
		})
	}
}

func TestBuild_ReferrerAndIP(t *testing.T) {
	tests := []struct {
		desc         string
		referrer     string
		forwardedFor string
		remoteAddr   string
		wantReferrer string
		wantIP       string
	}{
		{
			desc: "no referrer means Direct, remote addr with port",
			remoteAddr: "203.0.113.7:52814", wantReferrer: "Direct", wantIP: "203.0.113.7",
		},
		{
			desc:     "first valid forwarded IP wins",
			referrer: "https://t.co/abc", forwardedFor: "junk, 198.51.100.4, 10.0.0.1",
			remoteAddr: "127.0.0.1:1", wantReferrer: "https://t.co/abc", wantIP: "198.51.100.4",
		},
		{
			desc:         "ipv6 forwarded",
			forwardedFor: "2001:db8::1", remoteAddr: "127.0.0.1:1",
			wantReferrer: "Direct", wantIP: "2001:db8::1",
		},
		{
			desc:       "remote addr without port",
			remoteAddr: "198.51.100.9", wantReferrer: "Direct", wantIP: "198.51.100.9",
		},
		{
			desc:       "garbage everywhere degrades to empty ip",
			remoteAddr: "not-an-ip", forwardedFor: "also junk",
			wantReferrer: "Direct", wantIP: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			env := Build("", tc.referrer, tc.forwardedFor, tc.remoteAddr)

			assert.Equal(t, tc.wantReferrer, env.Referrer)
			assert.Equal(t, tc.wantIP, env.IP)
		})
	}
}

func TestEnv_ReferrerHost(t *testing.T) {
	tests := []struct {
		desc     string
		referrer string
		want     string
	}{
		{"direct has no host", DirectReferrer, ""},
		{"empty has no host", "", ""},
		{"www is stripped", "https://www.google.com/search?q=x", "google.com"},
		{"plain host", "https://t.co/abc", "t.co"},
		{"case folded", "https://News.YCombinator.com/item", "news.ycombinator.com"},
		{"schemeless referrer has no host", "some-junk", ""},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			assert.Equal(t, tc.want, Env{Referrer: tc.referrer}.ReferrerHost())
		})
	}
}
