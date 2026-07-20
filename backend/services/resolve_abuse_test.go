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

func disabledLink(id int64, code string) *models.Link {
	l := publicLink(id, code, "https://flagged.example/x")
	l.Status = models.LinkStatusDisabledAbuse

	return l
}

func TestResolveService_DisabledLinkIs410NoClick(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	svc.abuseContact = "abuse@sho.rt"

	// GetByCode only — no visit counter, no click row, no geo, no rules.
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(disabledLink(9, "abc1234"), nil)

	_, err := svc.Resolve(ctx, "abc1234", "", visitor.Env{IP: "9.9.9.9"})

	require.Error(t, err)

	var disabled *DisabledLinkError
	require.ErrorAs(t, err, &disabled)
	assert.Equal(t, http.StatusGone, disabled.StatusCode())
	assert.Equal(t, "abc1234", disabled.Code)
	assert.Equal(t, "abuse@sho.rt", disabled.AbuseContact)
}

func TestResolveService_GuessThrottle(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	svc.throttle = ratelimit.NewGuessThrottle(2, time.Minute, time.Minute)

	env := visitor.Env{IP: "5.5.5.5"}

	// Three unknown-code lookups: 404, 404, 404 — and the throttle trips.
	m.links.EXPECT().GetByCode(gomock.Any(), gomock.Any()).Return(nil, nil).Times(3)

	for range 3 {
		_, err := svc.Resolve(ctx, "nope", "", env)
		assertStatus(t, err, http.StatusNotFound)
	}

	// Fourth attempt never reaches the store: 429 during cooldown.
	_, err := svc.Resolve(ctx, "whatever", "", env)
	assertStatus(t, err, http.StatusTooManyRequests)

	// A different IP is unaffected (valid code resolves normally).
	stubNoTargeting(m)
	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(publicLink(9, "abc1234", "https://x.co"), nil)
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil)
	m.clicks.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil)

	_, err = svc.Resolve(ctx, "abc1234", "", visitor.Env{IP: "6.6.6.6"})
	require.NoError(t, err)
}

func TestResolveService_SuccessfulResolvesNeverCountAsMisses(t *testing.T) {
	svc, m, ctx := newResolveService(t)
	svc.throttle = ratelimit.NewGuessThrottle(2, time.Minute, time.Minute)

	stubNoTargeting(m)

	env := visitor.Env{IP: "5.5.5.5"}

	m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
		Return(publicLink(9, "abc1234", "https://x.co"), nil).Times(5)
	m.links.EXPECT().RecordVisit(gomock.Any(), int64(9)).Return(nil).Times(5)
	m.clicks.EXPECT().Insert(gomock.Any(), gomock.Any()).Return(nil).Times(5)

	for range 5 {
		_, err := svc.Resolve(ctx, "abc1234", "", env)
		require.NoError(t, err)
	}
}

func TestResolveService_PreviewByCode(t *testing.T) {
	t.Run("active link: destination returned, NO click recorded", func(t *testing.T) {
		svc, m, ctx := newResolveService(t)

		// Only the lookup — Insert/RecordVisit/geo/rules must never fire
		// (unstubbed mocks would fail the test if they did).
		m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").
			Return(publicLink(9, "abc1234", "https://x.co/page"), nil)

		got, err := svc.PreviewByCode(ctx, "abc1234", visitor.Env{IP: "1.2.3.4"})

		require.NoError(t, err)
		assert.Equal(t, "abc1234", got.Code)
		assert.Equal(t, "https://x.co/page", got.DestinationURL)
		assert.False(t, got.Disabled)
	})

	t.Run("disabled link previews with the disabled flag", func(t *testing.T) {
		svc, m, ctx := newResolveService(t)

		m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(disabledLink(9, "abc1234"), nil)

		got, err := svc.PreviewByCode(ctx, "abc1234", visitor.Env{})

		require.NoError(t, err)
		assert.True(t, got.Disabled)
	})

	t.Run("unknown code is 404 and counts as a guess miss", func(t *testing.T) {
		svc, m, ctx := newResolveService(t)
		svc.throttle = ratelimit.NewGuessThrottle(1, time.Minute, time.Minute)

		env := visitor.Env{IP: "5.5.5.5"}

		m.links.EXPECT().GetByCode(gomock.Any(), gomock.Any()).Return(nil, nil).Times(2)

		for range 2 {
			_, err := svc.PreviewByCode(ctx, "nope", env)
			assertStatus(t, err, http.StatusNotFound)
		}

		_, err := svc.PreviewByCode(ctx, "nope", env)
		assertStatus(t, err, http.StatusTooManyRequests)
	})

	t.Run("custom-domain scoping: another org's code is 404", func(t *testing.T) {
		svc, m, ctx := newResolveService(t)

		env := visitor.Env{Host: "links.example.com"}

		m.domains.EXPECT().GetByHostname(gomock.Any(), "links.example.com").
			Return(&models.Domain{ID: 1, OrgID: 7, Hostname: "links.example.com",
				Status: models.DomainStatusVerified}, nil).Times(2)

		// Own-org link previews.
		ownLink := publicLink(9, "abc1234", "https://x.co")
		ownLink.OrgID = 7
		m.links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(ownLink, nil)

		got, err := svc.PreviewByCode(ctx, "abc1234", env)
		require.NoError(t, err)
		assert.Equal(t, "https://x.co", got.DestinationURL)

		// Another org's code: same 404 as unknown (existence hidden).
		otherLink := publicLink(4, "zzz9999", "https://other.example")
		otherLink.OrgID = 8
		m.links.EXPECT().GetByCode(gomock.Any(), "zzz9999").Return(otherLink, nil)

		_, err = svc.PreviewByCode(ctx, "zzz9999", env)
		assertStatus(t, err, http.StatusNotFound)
	})
}
