package services

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/geo"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/ratelimit"
	"github.com/opengittr/ogtr/backend/visitor"
)

type resolveMocks struct {
	links   *MockLinkStore
	clicks  *MockClickStore
	rules   *MockRuleStore
	domains *MockDomainStore
	geo     *MockLocationResolver
}

func newResolveService(t *testing.T) (*ResolveService, resolveMocks, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	m := resolveMocks{
		links:   NewMockLinkStore(ctrl),
		clicks:  NewMockClickStore(ctrl),
		rules:   NewMockRuleStore(ctrl),
		domains: NewMockDomainStore(ctrl),
		geo:     NewMockLocationResolver(ctrl),
	}

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	// SHORT_DOMAIN carries a port like local dev does — host classification
	// must strip it. Envs without a Host stay in the global namespace, so
	// the pre-custom-domain tests never touch the domain store. The guess
	// throttle is real but generous enough to never trip in these tests
	// (throttle-specific tests build their own).
	throttle := ratelimit.NewGuessThrottle(1000, time.Minute, time.Minute)

	return NewResolveService(m.links, m.clicks, m.rules, m.domains, m.geo, throttle, "ogtr", "sho.rt:5810", ""), m, ctx
}

// stubNoTargeting makes GeoIP resolve nothing and the link carry no rules —
// the pre-phase-4 baseline behavior.
func stubNoTargeting(m resolveMocks) {
	m.geo.EXPECT().LocationForIP(gomock.Any()).Return(geo.Location{}).AnyTimes()
	m.rules.EXPECT().ListByLink(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
}

// captureClick records the inserted click for assertions.
func captureClick(m resolveMocks, into *models.Click) {
	m.clicks.EXPECT().Insert(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, c *models.Click) error {
			*into = *c

			return nil
		})
}

func mobileEnv(referrer string) visitor.Env {
	if referrer == "" {
		referrer = visitor.DirectReferrer
	}

	return visitor.Env{
		DeviceType: visitor.DeviceMobile,
		MobileOS:   visitor.OSIOS,
		Browser:    visitor.BrowserSafari,
		Referrer:   referrer,
		IP:         "203.0.113.9",
	}
}

func envWithOS(os string) visitor.Env {
	env := mobileEnv("")
	env.MobileOS = os

	if os == visitor.OSNotApplicable {
		env.DeviceType = visitor.DeviceDesktop
		env.Browser = visitor.BrowserChrome
	}

	return env
}

func deeplinkedLink(id int64, code, dest string) *models.Link {
	link := publicLink(id, code, dest)
	link.Deeplink = &models.DeeplinkConfig{
		Android: &models.AndroidDeeplink{
			Intent:      "shorturl/open",
			Package:     "com.example.app",
			Scheme:      "exampleapp",
			FallbackURL: "https://example.com/get-app?src=a b",
		},
		IOS: &models.IOSDeeplink{Intent: "https://apps.apple.com/app/id1"},
	}

	return link
}

func TestResolveService_UnknownCode(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	m.links.EXPECT().GetByCode(gomock.Any(), "nope").Return(nil, nil)

	_, err := svc.Resolve(ctx, "nope", "", mobileEnv(""))

	require.Error(t, err)
	assertStatus(t, err, http.StatusNotFound)
}

func TestResolveService_AutoUTMSources(t *testing.T) {
	tests := []struct {
		desc       string
		referrer   string
		wantSource string
	}{
		{"google referrer", "https://www.google.com/search?q=x", "google"},
		{"t.co referrer", "https://t.co/abc", "twitter"},
		{"twitter.com referrer", "https://twitter.com/x/status/1", "twitter"},
		{"facebook referrer", "https://facebook.com/groups/1", "facebook"},
		{"other referrer uses its host", "https://news.ycombinator.com/item?id=1", "news.ycombinator.com"},
		{"direct uses the self source", "", "ogtr"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newResolveService(t)
			stubNoTargeting(m)
			m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
				Return(publicLink(9, "abc1234", "https://example.com/p?x=1"), nil)
			m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

			var recorded models.Click

			captureClick(m, &recorded)

			res, err := svc.Resolve(ctx, "abc1234", "", mobileEnv(tc.referrer))

			require.NoError(t, err)
			assert.Contains(t, res.URL, "utm_source="+tc.wantSource)
			assert.Contains(t, res.URL, "utm_medium=referrer+by+Mobile")
			assert.Contains(t, res.URL, "x=1", "existing query must be preserved")
			assert.Equal(t, tc.wantSource, recorded.UTMSource)
			assert.Equal(t, "referrer by Mobile", recorded.UTMMedium)
		})
	}
}

func TestResolveService_ExplicitUTMsSkipAutoTag(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	stubNoTargeting(m)

	link := publicLink(9, "abc1234", "https://example.com/p?utm_source=tw")
	link.UTMSource = strPtr("tw")

	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(link, nil)
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

	var recorded models.Click

	captureClick(m, &recorded)

	res, err := svc.Resolve(ctx, "abc1234", "", mobileEnv("https://google.com"))

	require.NoError(t, err)
	assert.Equal(t, "https://example.com/p?utm_source=tw", res.URL, "no auto-tagging on explicit-UTM links")
	assert.Equal(t, "tw", recorded.UTMSource)
	assert.Empty(t, recorded.UTMMedium)
}

func TestResolveService_ClickRecordsEnvironmentAndTag(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	m.rules.EXPECT().ListByLink(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, nil)
	m.geo.EXPECT().LocationForIP("203.0.113.9").
		Return(geo.Location{City: "Bengaluru", Region: "Karnataka", Country: "India", CountryCode: "IN"})
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://example.com"), nil)
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

	var recorded models.Click

	captureClick(m, &recorded)

	_, err := svc.Resolve(ctx, "abc1234", "campaign-42", mobileEnv("https://t.co/x"))

	require.NoError(t, err)
	assert.Equal(t, int64(3), recorded.OrgID)
	assert.Equal(t, int64(9), recorded.LinkID)
	assert.Equal(t, "Mobile", recorded.DeviceType)
	assert.Equal(t, "iOS", recorded.MobileOS)
	assert.Equal(t, "Safari", recorded.Browser)
	assert.Equal(t, "https://t.co/x", recorded.Referrer)
	assert.Equal(t, "203.0.113.9", recorded.IP)
	assert.Equal(t, "campaign-42", recorded.CustomTagID)
	assert.Equal(t, "Bengaluru", recorded.City, "GeoIP city is recorded on the click")
	assert.Equal(t, "Karnataka", recorded.Region, "GeoIP region is recorded on the click")
	assert.Equal(t, "India", recorded.Country, "GeoIP country is recorded on the click")
	assert.False(t, recorded.IsDeeplink)
	assert.False(t, recorded.TargetMatched)
}

func TestResolveService_RecordingFailuresNeverBlockTheRedirect(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	stubNoTargeting(m)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://example.com"), nil)
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(assert.AnError)
	m.clicks.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(assert.AnError)

	res, err := svc.Resolve(ctx, "abc1234", "", mobileEnv(""))

	require.NoError(t, err)
	assert.Contains(t, res.URL, "https://example.com")
	assert.Equal(t, "abc1234", res.Code)
}

func TestResolveService_StoreError(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(nil, assert.AnError)

	_, err := svc.Resolve(ctx, "abc1234", "", mobileEnv(""))
	require.Error(t, err)
}

// --- Deep links (FEATURES.md §3.2) -----------------------------------------

func TestResolveService_Deeplink(t *testing.T) {
	wantAndroid := "intent:shorturl/open#Intent;package=com.example.app;scheme=exampleapp;" +
		"S.browser_fallback_url=https%3A%2F%2Fexample.com%2Fget-app%3Fsrc%3Da+b;end;"

	tests := []struct {
		desc         string
		os           string
		wantURL      string
		wantDeeplink bool
	}{
		{"android visitor gets the intent URI", visitor.OSAndroid, wantAndroid, true},
		{"ios visitor gets the ios intent", visitor.OSIOS, "https://apps.apple.com/app/id1", true},
		{"desktop visitor gets the destination", visitor.OSNotApplicable, "https://example.com", false},
		{"windows phone gets the destination", visitor.OSWindows, "https://example.com", false},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newResolveService(t)
			stubNoTargeting(m)
			m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
				Return(deeplinkedLink(9, "abc1234", "https://example.com"), nil)
			m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

			var recorded models.Click

			captureClick(m, &recorded)

			res, err := svc.Resolve(ctx, "abc1234", "", envWithOS(tc.os))

			require.NoError(t, err)
			assert.Equal(t, tc.wantDeeplink, recorded.IsDeeplink)

			if tc.wantDeeplink {
				assert.Equal(t, tc.wantURL, res.URL, "app links are served verbatim, no UTM tagging")
			} else {
				assert.Contains(t, res.URL, tc.wantURL, "non-matching OS falls through to the destination")
			}
		})
	}
}

func TestResolveService_DeeplinkSinglePlatform(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	stubNoTargeting(m)

	link := publicLink(9, "abc1234", "https://example.com")
	link.Deeplink = &models.DeeplinkConfig{IOS: &models.IOSDeeplink{Intent: "https://apps.apple.com/app/id1"}}

	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(link, nil)
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

	var recorded models.Click

	captureClick(m, &recorded)

	// Android visitor, but only an iOS config: no deep link.
	res, err := svc.Resolve(ctx, "abc1234", "", envWithOS(visitor.OSAndroid))

	require.NoError(t, err)
	assert.False(t, recorded.IsDeeplink)
	assert.Contains(t, res.URL, "https://example.com")
}

// --- Target rules (FEATURES.md §3.1) ----------------------------------------

func deviceRule(id int64, condType, value, dest string) models.Rule {
	return models.Rule{
		ID: id, OrgID: 3, LinkID: 9, TargetName: "by device",
		DeviceType: &models.RuleCondition{Type: condType, Values: []string{value}},
		URL:        dest,
	}
}

func TestResolveService_TargetRules(t *testing.T) {
	androidToStore := deviceRule(1, models.ConditionIs, "android", "https://play.example.com")
	notAndroid := deviceRule(2, models.ConditionIsNot, "Android", "https://web.example.com")
	multi := models.Rule{
		ID: 3, OrgID: 3, LinkID: 9, TargetName: "android in bengaluru",
		DeviceType: &models.RuleCondition{Type: models.ConditionIs, Values: []string{"Android"}},
		Location:   &models.RuleCondition{Type: models.ConditionIs, Values: []string{"bengaluru"}},
		URL:        "https://blr.example.com",
	}
	cityOnly := models.Rule{
		ID: 4, OrgID: 3, LinkID: 9, TargetName: "delhi",
		Location: &models.RuleCondition{Type: models.ConditionIs, Values: []string{"Delhi", "New Delhi"}},
		URL:      "https://delhi.example.com",
	}

	tests := []struct {
		desc        string
		rules       []models.Rule
		os          string
		city        string
		wantURL     string
		wantMatched bool
	}{
		{
			desc:  "device is-match wins, case-insensitively",
			rules: []models.Rule{androidToStore},
			os:    visitor.OSAndroid, city: "",
			wantURL: "https://play.example.com", wantMatched: true,
		},
		{
			desc:  "device is miss falls through",
			rules: []models.Rule{androidToStore},
			os:    visitor.OSIOS, city: "",
			wantURL: "https://example.com", wantMatched: false,
		},
		{
			desc:  "is_not matches a different OS",
			rules: []models.Rule{notAndroid},
			os:    visitor.OSIOS, city: "",
			wantURL: "https://web.example.com", wantMatched: true,
		},
		{
			desc:  "is_not does not match the listed OS",
			rules: []models.Rule{notAndroid},
			os:    visitor.OSAndroid, city: "",
			wantURL: "https://example.com", wantMatched: false,
		},
		{
			desc:  "multi-condition rule needs every condition",
			rules: []models.Rule{multi},
			os:    visitor.OSAndroid, city: "Mumbai",
			wantURL: "https://example.com", wantMatched: false,
		},
		{
			desc:  "multi-condition rule matches when all conditions hold",
			rules: []models.Rule{multi},
			os:    visitor.OSAndroid, city: "Bengaluru",
			wantURL: "https://blr.example.com", wantMatched: true,
		},
		{
			desc:  "city list matches any value",
			rules: []models.Rule{cityOnly},
			os:    visitor.OSNotApplicable, city: "new delhi",
			wantURL: "https://delhi.example.com", wantMatched: true,
		},
		{
			desc:  "unknown city never matches a location condition, even is_not",
			rules: []models.Rule{{ID: 5, OrgID: 3, LinkID: 9, TargetName: "not blr", Location: &models.RuleCondition{Type: models.ConditionIsNot, Values: []string{"Bengaluru"}}, URL: "https://elsewhere.example.com"}},
			os:    visitor.OSAndroid, city: "",
			wantURL: "https://example.com", wantMatched: false,
		},
		{
			desc:  "first matching rule wins over a later one",
			rules: []models.Rule{androidToStore, deviceRule(9, models.ConditionIs, "Android", "https://second.example.com")},
			os:    visitor.OSAndroid, city: "",
			wantURL: "https://play.example.com", wantMatched: true,
		},
		{
			desc:  "first rule misses, second matches",
			rules: []models.Rule{multi, androidToStore},
			os:    visitor.OSAndroid, city: "Mumbai",
			wantURL: "https://play.example.com", wantMatched: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newResolveService(t)
			m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
				Return(publicLink(9, "abc1234", "https://example.com"), nil)
			m.geo.EXPECT().LocationForIP(gomock.Any()).Return(geo.Location{City: tc.city})
			m.rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).Return(tc.rules, nil)
			m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

			var recorded models.Click

			captureClick(m, &recorded)

			res, err := svc.Resolve(ctx, "abc1234", "", envWithOS(tc.os))

			require.NoError(t, err)
			assert.Equal(t, tc.wantMatched, recorded.TargetMatched)
			assert.Contains(t, res.URL, tc.wantURL)

			if tc.wantMatched {
				assert.Equal(t, tc.wantURL, res.URL, "rule URLs are served verbatim, no UTM tagging")
			}
		})
	}
}

func TestResolveService_RuleOverridesDeeplinkURL(t *testing.T) {
	// Parity: deep link and rules both evaluate; a matching rule's URL wins,
	// and both flags are recorded.
	svc, m, ctx := newResolveService(t)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(deeplinkedLink(9, "abc1234", "https://example.com"), nil)
	m.geo.EXPECT().LocationForIP(gomock.Any()).Return(geo.Location{})
	m.rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).
		Return([]models.Rule{deviceRule(1, models.ConditionIs, "Android", "https://rule.example.com")}, nil)
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

	var recorded models.Click

	captureClick(m, &recorded)

	res, err := svc.Resolve(ctx, "abc1234", "", envWithOS(visitor.OSAndroid))

	require.NoError(t, err)
	assert.Equal(t, "https://rule.example.com", res.URL)
	assert.True(t, recorded.IsDeeplink)
	assert.True(t, recorded.TargetMatched)
}

func TestResolveService_RuleListingFailureIsSkipped(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://example.com"), nil)
	m.geo.EXPECT().LocationForIP(gomock.Any()).Return(geo.Location{})
	m.rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).Return(nil, assert.AnError)
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

	var recorded models.Click

	captureClick(m, &recorded)

	res, err := svc.Resolve(ctx, "abc1234", "", mobileEnv(""))

	require.NoError(t, err, "a rules read failure must never block the redirect")
	assert.Contains(t, res.URL, "https://example.com")
	assert.False(t, recorded.TargetMatched)
}

func TestResolveService_LegacyRuleWithoutConditionsNeverMatches(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://example.com"), nil)
	m.geo.EXPECT().LocationForIP(gomock.Any()).Return(geo.Location{City: "Bengaluru"})
	m.rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).
		Return([]models.Rule{{ID: 1, OrgID: 3, LinkID: 9, TargetName: "broken", URL: "https://x.example.com"}}, nil)
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

	var recorded models.Click

	captureClick(m, &recorded)

	res, err := svc.Resolve(ctx, "abc1234", "", mobileEnv(""))

	require.NoError(t, err)
	assert.Contains(t, res.URL, "https://example.com")
	assert.False(t, recorded.TargetMatched, "a condition-less rule must not catch all traffic")
}

func strPtr(s string) *string { return &s }

// --- Host-aware resolution (custom domains, FEATURES.md §1.6) ---------------

func hostEnv(host string) visitor.Env {
	env := mobileEnv("")
	env.Host = host

	return env
}

func customDomain(orgID int64, status string) *models.Domain {
	return &models.Domain{ID: 21, OrgID: orgID, Hostname: "links.example.com", Status: status}
}

func TestResolveService_CustomDomainResolvesOwnOrg(t *testing.T) {
	tests := []struct {
		desc string
		host string
	}{
		{"exact host", "links.example.com"},
		{"host with port is stripped", "links.example.com:8443"},
		{"host is matched case-insensitively", "LINKS.Example.COM"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newResolveService(t)
			stubNoTargeting(m)
			m.domains.EXPECT().GetByHostname(gomock.Any(), "links.example.com").
				Return(customDomain(3, models.DomainStatusVerified), nil)
			m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
				Return(publicLink(9, "abc1234", "https://example.com"), nil) // link org = 3
			m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

			var recorded models.Click

			captureClick(m, &recorded)

			res, err := svc.Resolve(ctx, "abc1234", "", hostEnv(tc.host))

			require.NoError(t, err)
			assert.Contains(t, res.URL, "https://example.com")
			assert.Equal(t, int64(3), recorded.OrgID, "the click still records normally")
		})
	}
}

func TestResolveService_CustomDomainHidesOtherOrgsCodes(t *testing.T) {
	// The code exists but belongs to org 3; the domain belongs to org 99 —
	// same 404 as an unknown code, and neither counter nor click is written.
	svc, m, ctx := newResolveService(t)
	m.domains.EXPECT().GetByHostname(gomock.Any(), "links.example.com").
		Return(customDomain(99, models.DomainStatusVerified), nil)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://example.com"), nil)

	_, err := svc.Resolve(ctx, "abc1234", "", hostEnv("links.example.com"))

	require.Error(t, err)
	assertStatus(t, err, http.StatusNotFound)
}

func TestResolveService_NonServingHostsAre404(t *testing.T) {
	tests := []struct {
		desc   string
		domain *models.Domain
	}{
		{"unknown host", nil},
		{"pending domain", customDomain(3, models.DomainStatusPending)},
		{"disabled domain", customDomain(3, models.DomainStatusDisabled)},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newResolveService(t)
			m.domains.EXPECT().GetByHostname(gomock.Any(), "links.example.com").Return(tc.domain, nil)

			// The link is never even looked up (no GetByCode expectation).
			_, err := svc.Resolve(ctx, "abc1234", "", hostEnv("links.example.com"))

			require.Error(t, err)
			assertStatus(t, err, http.StatusNotFound)
		})
	}
}

func TestResolveService_DeploymentHostsStayGlobal(t *testing.T) {
	// SHORT_DOMAIN (with or without port) and local-dev loopback hosts keep
	// the global namespace: any org's code resolves and the domain store is
	// never consulted (no GetByHostname expectation).
	for _, host := range []string{"", "sho.rt", "sho.rt:5810", "localhost:5810", "127.0.0.1:5810"} {
		t.Run("host "+host, func(t *testing.T) {
			svc, m, ctx := newResolveService(t)
			stubNoTargeting(m)
			m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
				Return(publicLink(9, "abc1234", "https://example.com"), nil)
			m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

			var recorded models.Click

			captureClick(m, &recorded)

			res, err := svc.Resolve(ctx, "abc1234", "", hostEnv(host))

			require.NoError(t, err)
			assert.Contains(t, res.URL, "https://example.com")
		})
	}
}

func TestResolveService_DomainLookupErrorFailsClosed(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	m.domains.EXPECT().GetByHostname(gomock.Any(), "links.example.com").Return(nil, assert.AnError)

	_, err := svc.Resolve(ctx, "abc1234", "", hostEnv("links.example.com"))
	require.Error(t, err)
}
