package services

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

func TestLinkService_PrefixDisplay(t *testing.T) {
	t.Run("primary approved prefix joins the deployment base", func(t *testing.T) {
		svc, links, _, domains, prefixes, ctx := newLinkServiceWithPrefixes(t, limits.Unlimited{})
		domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("", nil)
		prefixes.EXPECT().PrimaryApprovedPrefix(gomock.Any(), int64(3)).Return("ACMEIN", nil)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://example.com"), nil)

		got, err := svc.Get(ctx, 3, 7, 9)

		require.NoError(t, err)
		assert.Equal(t, "http://sho.rt/ACMEIN/abc1234", got.ShortURL)
	})

	t.Run("primary custom domain takes precedence (no prefix lookup)", func(t *testing.T) {
		svc, links, _, domains, _, ctx := newLinkServiceWithPrefixes(t, limits.Unlimited{})
		domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("links.example.com", nil)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://example.com"), nil)

		got, err := svc.Get(ctx, 3, 7, 9)

		require.NoError(t, err)
		assert.Equal(t, "https://links.example.com/abc1234", got.ShortURL)
	})

	t.Run("prefix lookup failure falls back, never blocks", func(t *testing.T) {
		svc, links, _, domains, prefixes, ctx := newLinkServiceWithPrefixes(t, limits.Unlimited{})
		domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("", nil)
		prefixes.EXPECT().PrimaryApprovedPrefix(gomock.Any(), int64(3)).Return("", assert.AnError)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://example.com"), nil)

		got, err := svc.Get(ctx, 3, 7, 9)

		require.NoError(t, err)
		assert.Equal(t, "http://sho.rt/abc1234", got.ShortURL)
	})
}

func TestLinkService_SetAlias_HeldPrefixConflicts(t *testing.T) {
	// No link store access: the alias is refused before any lookup.
	svc, _, _, domains, prefixes, ctx := newLinkServiceWithPrefixes(t, limits.Unlimited{})
	domains.EXPECT().HasVerified(gomock.Any(), int64(3)).Return(false, nil)
	prefixes.EXPECT().IsHeld(gomock.Any(), "acmein").Return(true, nil)

	_, err := svc.SetAlias(ctx, 3, 7, 9, "acmein")

	assertStatus(t, err, http.StatusConflict)
}

func TestLinkService_UniqueCode_RetriesHeldPrefixes(t *testing.T) {
	svc, links, _, _, prefixes, ctx := newLinkServiceWithPrefixes(t, limits.Unlimited{})

	draws := []string{"ACMEIN1", "abc1234"}
	randomCodeFn = func(int) (string, error) {
		next := draws[0]
		draws = draws[1:]

		return next, nil
	}

	t.Cleanup(func() { randomCodeFn = randomCode })

	prefixes.EXPECT().IsHeld(gomock.Any(), "ACMEIN1").Return(true, nil)
	prefixes.EXPECT().IsHeld(gomock.Any(), "abc1234").Return(false, nil)
	links.EXPECT().CodeExists(gomock.Any(), "abc1234").Return(false, nil)

	code, err := svc.uniqueCode(ctx)

	require.NoError(t, err)
	assert.Equal(t, "abc1234", code)
}

func TestLinkService_Shorten_AlreadyShortUnderOwnPrefix(t *testing.T) {
	t.Run("own approved prefix returns the existing link", func(t *testing.T) {
		svc, links, _, domains, prefixes, ctx := newLinkServiceWithPrefixes(t, limits.Unlimited{})
		domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("", nil).AnyTimes()
		prefixes.EXPECT().PrimaryApprovedPrefix(gomock.Any(), int64(3)).Return("ACMEIN", nil).AnyTimes()
		prefixes.EXPECT().GetByPrefix(gomock.Any(), "ACMEIN").
			Return(&models.BrandPrefix{ID: 5, OrgID: 3, Prefix: "ACMEIN", Status: models.BrandPrefixStatusApproved}, nil)
		links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(publicLink(9, "abc1234", "https://example.com"), nil)

		got, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "http://sho.rt/ACMEIN/abc1234"})

		require.NoError(t, err)
		assert.Equal(t, int64(9), got.ID)
		assert.Equal(t, "http://sho.rt/ACMEIN/abc1234", got.ShortURL)
	})

	t.Run("another org's prefix is refused", func(t *testing.T) {
		svc, _, _, _, prefixes, ctx := newLinkServiceWithPrefixes(t, limits.Unlimited{})
		prefixes.EXPECT().GetByPrefix(gomock.Any(), "OTHER").
			Return(&models.BrandPrefix{ID: 6, OrgID: 99, Prefix: "OTHER", Status: models.BrandPrefixStatusApproved}, nil)

		_, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "http://sho.rt/OTHER/abc1234"})

		assertStatus(t, err, http.StatusUnprocessableEntity)
	})
}
