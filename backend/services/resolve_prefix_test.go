package services

import (
	"net/http"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/ratelimit"
	"github.com/opengittr/ogtr/backend/visitor"
)

func brandPrefix(id, orgID int64, prefix, status string) *models.BrandPrefix {
	return &models.BrandPrefix{ID: id, OrgID: orgID, Prefix: prefix, Status: status}
}

func TestResolveService_PrefixResolvesOwnOrgAndRecordsEntryPoint(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	stubNoTargeting(m)
	m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "acmein").
		Return(brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusApproved), nil)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://example.com"), nil) // link org = 3
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

	var recorded models.Click

	captureClick(m, &recorded)

	res, err := svc.Resolve(ctx, "acmein", "abc1234", "", mobileEnv(""))

	require.NoError(t, err)
	assert.Contains(t, res.URL, "https://example.com")
	assert.Equal(t, models.EntryPointPrefix, recorded.EntryPoint)
	assert.Equal(t, int64(5), recorded.BrandPrefixID)
}

func TestResolveService_RootAndDomainEntryPoints(t *testing.T) {
	t.Run("root", func(t *testing.T) {
		svc, m, ctx := newResolveService(t)
		stubNoTargeting(m)
		m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
			Return(publicLink(9, "abc1234", "https://example.com"), nil)
		m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

		var recorded models.Click

		captureClick(m, &recorded)

		_, err := svc.Resolve(ctx, "", "abc1234", "", mobileEnv(""))

		require.NoError(t, err)
		assert.Equal(t, models.EntryPointRoot, recorded.EntryPoint)
		assert.Zero(t, recorded.BrandPrefixID)
	})

	t.Run("custom domain", func(t *testing.T) {
		svc, m, ctx := newResolveService(t)
		stubNoTargeting(m)
		m.domains.EXPECT().GetByHostname(gomock.Any(), "links.example.com").
			Return(customDomain(3, models.DomainStatusVerified), nil)
		m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
			Return(publicLink(9, "abc1234", "https://example.com"), nil)
		m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)

		var recorded models.Click

		captureClick(m, &recorded)

		_, err := svc.Resolve(ctx, "", "abc1234", "", hostEnv("links.example.com"))

		require.NoError(t, err)
		assert.Equal(t, models.EntryPointDomain, recorded.EntryPoint)
	})
}

func TestResolveService_PrefixHidesOtherOrgsCodes(t *testing.T) {
	// The code exists in org 3; the prefix belongs to org 99 — the same 404
	// as an unknown code, nothing recorded.
	svc, m, ctx := newResolveService(t)
	m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "OTHER").
		Return(brandPrefix(6, 99, "OTHER", models.BrandPrefixStatusApproved), nil)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://example.com"), nil)

	_, err := svc.Resolve(ctx, "OTHER", "abc1234", "", mobileEnv(""))

	assertStatus(t, err, http.StatusNotFound)
}

func TestResolveService_NonServingPrefixesAre404(t *testing.T) {
	tests := []struct {
		desc   string
		prefix *models.BrandPrefix
	}{
		{"unknown prefix", nil},
		{"pending", brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusPending)},
		{"rejected", brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusRejected)},
		{"revoked", brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusRevoked)},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newResolveService(t)
			m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "ACMEIN").Return(tc.prefix, nil)

			// No code lookup happens: the scope already failed.
			_, err := svc.Resolve(ctx, "ACMEIN", "abc1234", "", mobileEnv(""))

			assertStatus(t, err, http.StatusNotFound)
		})
	}
}

func TestResolveService_PrefixOnCustomDomainIs404(t *testing.T) {
	// Prefixes are served on SHORT_DOMAIN only: no prefix lookup at all.
	svc, m, ctx := newResolveService(t)
	m.domains.EXPECT().GetByHostname(gomock.Any(), "links.example.com").
		Return(customDomain(3, models.DomainStatusVerified), nil)

	_, err := svc.Resolve(ctx, "ACMEIN", "abc1234", "", hostEnv("links.example.com"))

	assertStatus(t, err, http.StatusNotFound)
}

func TestResolveService_PrefixLookupErrorFailsClosed(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "ACMEIN").Return(nil, assert.AnError)

	_, err := svc.Resolve(ctx, "ACMEIN", "abc1234", "", mobileEnv(""))

	require.ErrorIs(t, err, assert.AnError)
}

func TestResolveService_UnknownPrefixCountsAsGuessMiss(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	svc.throttle = ratelimit.NewGuessThrottle(2, time.Minute, time.Minute)
	m.prefixes.EXPECT().GetByPrefix(gomock.Any(), gomock.Any()).Return(nil, nil).Times(3)

	env := visitor.Env{IP: "9.9.9.9"}

	// Three unknown-prefix 404s trip the throttle like unknown codes do.
	for range 3 {
		_, err := svc.Resolve(ctx, "NOPE", "abc1234", "", env)
		assertStatus(t, err, http.StatusNotFound)
	}

	_, err := svc.Resolve(ctx, "NOPE", "abc1234", "", env)

	assertStatus(t, err, http.StatusTooManyRequests)
}

func TestResolveService_PreviewHonorsPrefixScope(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "ACMEIN").
		Return(brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusApproved), nil)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://example.com"), nil)

	preview, err := svc.PreviewByCode(ctx, "ACMEIN", "abc1234", mobileEnv(""))

	require.NoError(t, err)
	assert.Equal(t, "https://example.com", preview.DestinationURL)
}
