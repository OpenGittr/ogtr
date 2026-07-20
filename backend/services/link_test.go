package services

import (
	"bytes"
	"context"
	"net/http"
	"regexp"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/models"
)

func newLinkService(t *testing.T) (*LinkService, *MockLinkStore, *gofr.Context) {
	t.Helper()

	svc, links, _, ctx := newLinkServiceWithMembers(t)

	return svc, links, ctx
}

func newLinkServiceWithMembers(t *testing.T) (*LinkService, *MockLinkStore, *MockMemberStore, *gofr.Context) {
	t.Helper()

	svc, links, members, domains, ctx := newLinkServiceWithDomains(t)

	// Default for every pre-custom-domain test: the org has no primary
	// verified domain, so short URLs display under SHORT_DOMAIN as before —
	// and no verified domain at all, so the full reserved-alias set applies.
	domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()
	domains.EXPECT().HasVerified(gomock.Any(), gomock.Any()).Return(false, nil).AnyTimes()

	return svc, links, members, ctx
}

// newLinkServiceWithDomains exposes the domain-store mock (no stubbed
// expectations) for the short-URL display tests.
func newLinkServiceWithDomains(t *testing.T) (*LinkService, *MockLinkStore, *MockMemberStore, *MockDomainStore, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	links := NewMockLinkStore(ctrl)
	members := NewMockMemberStore(ctrl)
	domains := NewMockDomainStore(ctrl)

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	// nil scanner = allow every destination; scanning behavior has its own
	// tests with a mock URLScanner.
	return NewLinkService(links, members, domains, nil, nil, "http", "sho.rt", ""), links, members, domains, ctx
}

func publicLink(id int64, code, dest string) *models.Link {
	return &models.Link{ID: id, OrgID: 3, UserID: ptr64(7), Code: code, DestinationURL: dest, Type: models.LinkTypePublic}
}

// ptr64 builds a *int64 literal (nullable links.user_id / api_key_id).
func ptr64(v int64) *int64 { return &v }

var codeRe = regexp.MustCompile(`^[a-zA-Z0-9]{7}$`)

func TestLinkService_Shorten_CreatesWithNormalizedURL(t *testing.T) {
	tests := []struct {
		desc     string
		input    ShortenInput
		wantDest string
		wantType string
	}{
		{
			desc:     "schemeless URL is prefixed with https",
			input:    ShortenInput{URL: "example.com/page"},
			wantDest: "https://example.com/page",
			wantType: models.LinkTypePublic,
		},
		{
			desc:     "explicit http is kept",
			input:    ShortenInput{URL: "http://example.com"},
			wantDest: "http://example.com",
			wantType: models.LinkTypePublic,
		},
		{
			desc:     "private type is honored",
			input:    ShortenInput{URL: "https://example.com/secret", Type: "private"},
			wantDest: "https://example.com/secret",
			wantType: models.LinkTypePrivate,
		},
		{
			desc:     "utm params appended via net/url, preserving existing query",
			input:    ShortenInput{URL: "https://example.com/p?x=1", UTMSource: "news letter", UTMCampaign: "läunch"},
			wantDest: "https://example.com/p?utm_campaign=l%C3%A4unch&utm_source=news+letter&x=1",
			wantType: models.LinkTypePublic,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, links, ctx := newLinkService(t)

			links.EXPECT().FindByDestination(gomock.Any(), int64(3), int64(7), tc.wantDest).Return(nil, nil)
			links.EXPECT().CodeExists(gomock.Any(), gomock.Any()).Return(false, nil)
			links.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
				func(_ *gofr.Context, l *models.Link) (*models.Link, error) {
					assert.True(t, codeRe.MatchString(l.Code), "code %q should be 7 base62 chars", l.Code)
					assert.Equal(t, tc.wantDest, l.DestinationURL)
					assert.Equal(t, tc.wantType, l.Type)

					l.ID = 9

					return l, nil
				})

			got, err := svc.Shorten(ctx, 3, 7, tc.input)

			require.NoError(t, err)
			assert.Equal(t, tc.wantDest, got.DestinationURL)
			assert.Equal(t, "http://sho.rt/"+got.Code, got.ShortURL)
		})
	}
}

func TestLinkService_Shorten_StoresAsCreatedUTMs(t *testing.T) {
	svc, links, ctx := newLinkService(t)

	links.EXPECT().FindByDestination(gomock.Any(), int64(3), int64(7), gomock.Any()).Return(nil, nil)
	links.EXPECT().CodeExists(gomock.Any(), gomock.Any()).Return(false, nil)
	links.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, l *models.Link) (*models.Link, error) {
			require.NotNil(t, l.UTMSource)
			assert.Equal(t, "tw", *l.UTMSource)
			require.NotNil(t, l.UTMMedium)
			assert.Equal(t, "social", *l.UTMMedium)
			assert.Nil(t, l.UTMCampaign)

			return l, nil
		})

	_, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "https://example.com", UTMSource: "tw", UTMMedium: "social"})
	require.NoError(t, err)
}

func TestLinkService_Shorten_DedupesPerOrg(t *testing.T) {
	svc, links, ctx := newLinkService(t)
	existing := publicLink(9, "abc1234", "https://example.com/page")

	links.EXPECT().FindByDestination(gomock.Any(), int64(3), int64(7), "https://example.com/page").
		Return(existing, nil)

	got, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "example.com/page"})

	require.NoError(t, err)
	assert.Equal(t, int64(9), got.ID)
	assert.Equal(t, "http://sho.rt/abc1234", got.ShortURL)
}

func TestLinkService_Shorten_AlreadyShortReturnsExisting(t *testing.T) {
	svc, links, ctx := newLinkService(t)
	existing := publicLink(9, "abc1234", "https://example.com")

	links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(existing, nil)

	got, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "http://sho.rt/abc1234"})

	require.NoError(t, err)
	assert.Equal(t, int64(9), got.ID)
}

func TestLinkService_Shorten_Rejections(t *testing.T) {
	tests := []struct {
		desc  string
		input ShortenInput
		setup func(links *MockLinkStore)
	}{
		{desc: "empty url", input: ShortenInput{URL: "   "}},
		{desc: "malformed scheme", input: ShortenInput{URL: "https:/example.com"}},
		{desc: "non-http scheme", input: ShortenInput{URL: "ftp://example.com/file"}},
		{desc: "no host", input: ShortenInput{URL: "https://"}},
		{desc: "bad type", input: ShortenInput{URL: "https://example.com", Type: "SECRET"}},
		{
			desc:  "short-domain URL with unknown code",
			input: ShortenInput{URL: "http://sho.rt/nope123"},
			setup: func(links *MockLinkStore) {
				links.EXPECT().GetByCode(gomock.Any(), "nope123").Return(nil, nil)
			},
		},
		{
			desc:  "short-domain URL owned by another org",
			input: ShortenInput{URL: "http://sho.rt/abc1234"},
			setup: func(links *MockLinkStore) {
				other := publicLink(9, "abc1234", "https://x.co")
				other.OrgID = 99
				links.EXPECT().GetByCode(gomock.Any(), "abc1234").Return(other, nil)
			},
		},
		{desc: "bare short domain", input: ShortenInput{URL: "http://sho.rt/"}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, links, ctx := newLinkService(t)
			if tc.setup != nil {
				tc.setup(links)
			}

			_, err := svc.Shorten(ctx, 3, 7, tc.input)

			require.Error(t, err)
			assertStatus(t, err, http.StatusUnprocessableEntity)
		})
	}
}

func TestLinkService_ShortenViaAPIKey_AttributesToKey(t *testing.T) {
	svc, links, ctx := newLinkService(t)

	// Dedupe and already-short lookups run with viewer 0: an API key sees
	// only PUBLIC links.
	links.EXPECT().FindByDestination(gomock.Any(), int64(3), int64(0), "https://example.com/api").Return(nil, nil)
	links.EXPECT().CodeExists(gomock.Any(), gomock.Any()).Return(false, nil)
	links.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, l *models.Link) (*models.Link, error) {
			assert.Nil(t, l.UserID, "API-key-created links have no user")
			require.NotNil(t, l.APIKeyID)
			assert.Equal(t, int64(11), *l.APIKeyID)
			assert.Equal(t, models.LinkTypePublic, l.Type)

			return l, nil
		})

	got, err := svc.ShortenViaAPIKey(ctx, 3, 11, ShortenInput{URL: "https://example.com/api"})

	require.NoError(t, err)
	assert.Nil(t, got.UserID)
}

func TestLinkService_ShortenViaAPIKey_PrivateRejected(t *testing.T) {
	// A PRIVATE link needs a creator to be private to; API keys have none.
	svc, _, ctx := newLinkService(t)

	_, err := svc.ShortenViaAPIKey(ctx, 3, 11, ShortenInput{URL: "https://example.com", Type: "PRIVATE"})

	require.Error(t, err)
	assertStatus(t, err, http.StatusUnprocessableEntity)
}

func TestLinkService_Shorten_JWTPathKeepsUserAttribution(t *testing.T) {
	svc, links, ctx := newLinkService(t)

	links.EXPECT().FindByDestination(gomock.Any(), int64(3), int64(7), gomock.Any()).Return(nil, nil)
	links.EXPECT().CodeExists(gomock.Any(), gomock.Any()).Return(false, nil)
	links.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, l *models.Link) (*models.Link, error) {
			require.NotNil(t, l.UserID)
			assert.Equal(t, int64(7), *l.UserID)
			assert.Nil(t, l.APIKeyID)

			return l, nil
		})

	_, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "https://example.com"})
	require.NoError(t, err)
}

func TestLinkService_Shorten_RetriesCodeCollision(t *testing.T) {
	svc, links, ctx := newLinkService(t)

	links.EXPECT().FindByDestination(gomock.Any(), int64(3), int64(7), gomock.Any()).Return(nil, nil)
	links.EXPECT().CodeExists(gomock.Any(), gomock.Any()).Return(true, nil)
	links.EXPECT().CodeExists(gomock.Any(), gomock.Any()).Return(false, nil)
	links.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, l *models.Link) (*models.Link, error) { return l, nil })

	_, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "https://example.com"})
	require.NoError(t, err)
}

func TestLinkService_Get_Visibility(t *testing.T) {
	private := publicLink(9, "abc1234", "https://x.co")
	private.Type = models.LinkTypePrivate

	tests := []struct {
		desc     string
		viewerID int64
		link     *models.Link
		wantErr  bool
	}{
		{desc: "creator sees their private link", viewerID: 7, link: private},
		{desc: "another member gets 404 for a private link", viewerID: 8, link: private, wantErr: true},
		{desc: "missing link is 404", viewerID: 7, link: nil, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, links, ctx := newLinkService(t)
			links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(tc.link, nil)

			got, err := svc.Get(ctx, 3, tc.viewerID, 9)

			if tc.wantErr {
				require.Error(t, err)
				assertStatus(t, err, http.StatusNotFound)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, "http://sho.rt/abc1234", got.ShortURL)
		})
	}
}

func TestLinkService_List(t *testing.T) {
	svc, links, ctx := newLinkService(t)

	links.EXPECT().List(gomock.Any(), int64(3), int64(7), 10, 10).
		Return([]models.Link{*publicLink(9, "abc1234", "https://x.co")}, nil)
	links.EXPECT().Count(gomock.Any(), int64(3), int64(7)).Return(int64(11), nil)

	page, err := svc.List(ctx, 3, 7, 2)

	require.NoError(t, err)
	assert.Equal(t, 2, page.Page)
	assert.Equal(t, 10, page.PerPage)
	assert.Equal(t, int64(11), page.Total)
	require.Len(t, page.Links, 1)
	assert.Equal(t, "http://sho.rt/abc1234", page.Links[0].ShortURL)
}

func TestLinkService_List_DefaultsPage(t *testing.T) {
	svc, links, ctx := newLinkService(t)

	links.EXPECT().List(gomock.Any(), int64(3), int64(7), 10, 0).Return([]models.Link{}, nil)
	links.EXPECT().Count(gomock.Any(), int64(3), int64(7)).Return(int64(0), nil)

	page, err := svc.List(ctx, 3, 7, 0)

	require.NoError(t, err)
	assert.Equal(t, 1, page.Page)
}

func TestLinkService_SetAlias(t *testing.T) {
	tests := []struct {
		desc       string
		alias      string
		setup      func(links *MockLinkStore)
		wantStatus int
	}{
		{
			desc:  "valid alias is set; old code retired",
			alias: "my-Brand_1",
			setup: func(links *MockLinkStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", "https://x.co"), nil)
				links.EXPECT().CodeExists(gomock.Any(), "my-Brand_1").Return(false, nil)
				links.EXPECT().UpdateCode(gomock.Any(), int64(3), int64(9), "my-Brand_1").Return(nil)
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "my-Brand_1", "https://x.co"), nil)
			},
		},
		{desc: "too short", alias: "ab", wantStatus: http.StatusUnprocessableEntity},
		{desc: "bad characters", alias: "no spaces!", wantStatus: http.StatusUnprocessableEntity},
		{desc: "reserved word api", alias: "api", wantStatus: http.StatusUnprocessableEntity},
		{desc: "reserved word login (case-insensitive)", alias: "LOGIN", wantStatus: http.StatusUnprocessableEntity},
		{desc: "reserved word metrics", alias: "metrics", wantStatus: http.StatusUnprocessableEntity},
		{
			desc:  "taken code conflicts",
			alias: "taken12",
			setup: func(links *MockLinkStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", "https://x.co"), nil)
				links.EXPECT().CodeExists(gomock.Any(), "taken12").Return(true, nil)
			},
			wantStatus: http.StatusConflict,
		},
		{
			desc:  "same alias is a no-op",
			alias: "abc1234",
			setup: func(links *MockLinkStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", "https://x.co"), nil)
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, links, ctx := newLinkService(t)
			if tc.setup != nil {
				tc.setup(links)
			}

			got, err := svc.SetAlias(ctx, 3, 7, 9, tc.alias)

			if tc.wantStatus != 0 {
				require.Error(t, err)
				assertStatus(t, err, tc.wantStatus)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.alias, got.Code)
		})
	}
}

func TestValidateAlias_DotPrefixed(t *testing.T) {
	// The format regex already excludes dots; the explicit dot-prefix rule is
	// belt-and-braces per ARCHITECTURE.md §3.
	svc, _, ctx := newLinkService(t)

	require.Error(t, svc.validateAlias(ctx, 3, ".well-known"))
	require.Error(t, svc.validateAlias(ctx, 3, ".hidden"))
}

func TestLinkService_SetDeeplink(t *testing.T) {
	androidOnly := &models.DeeplinkConfig{
		Android: &models.AndroidDeeplink{
			Intent: "open", Package: "com.x", Scheme: "x", FallbackURL: "https://x.co/get",
		},
	}

	tests := []struct {
		desc       string
		cfg        *models.DeeplinkConfig
		setup      func(links *MockLinkStore)
		wantStatus int
	}{
		{
			desc: "valid android config is stored",
			cfg:  androidOnly,
			setup: func(links *MockLinkStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", "https://x.co"), nil)
				links.EXPECT().UpdateDeeplink(gomock.Any(), int64(3), int64(9), androidOnly).Return(nil)
				updated := publicLink(9, "abc1234", "https://x.co")
				updated.Deeplink = androidOnly
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(updated, nil)
			},
		},
		{
			desc: "nil config clears",
			cfg:  nil,
			setup: func(links *MockLinkStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", "https://x.co"), nil).Times(2)
				links.EXPECT().UpdateDeeplink(gomock.Any(), int64(3), int64(9), gomock.Nil()).Return(nil)
			},
		},
		{
			desc: "android config with a blank field is 422",
			cfg: &models.DeeplinkConfig{
				Android: &models.AndroidDeeplink{Intent: "open", Package: "com.x", Scheme: " "},
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			desc:       "ios config without intent is 422",
			cfg:        &models.DeeplinkConfig{IOS: &models.IOSDeeplink{Intent: ""}},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			desc: "missing link is 404",
			cfg:  androidOnly,
			setup: func(links *MockLinkStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(nil, nil)
			},
			wantStatus: http.StatusNotFound,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, links, ctx := newLinkService(t)
			if tc.setup != nil {
				tc.setup(links)
			}

			got, err := svc.SetDeeplink(ctx, 3, 7, 9, tc.cfg)

			if tc.wantStatus != 0 {
				require.Error(t, err)
				assertStatus(t, err, tc.wantStatus)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
		})
	}
}

func TestLinkService_SetDeeplink_PrivateLinkNonCreator(t *testing.T) {
	// INV-3 owner-only writes: another org member cannot even see a PRIVATE
	// link, so their deeplink write is 404 — existence stays hidden.
	private := publicLink(9, "abc1234", "https://x.co")
	private.Type = models.LinkTypePrivate
	private.UserID = ptr64(7)

	svc, links, ctx := newLinkService(t)
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(private, nil)

	_, err := svc.SetDeeplink(ctx, 3, 8, 9,
		&models.DeeplinkConfig{IOS: &models.IOSDeeplink{Intent: "https://apps.x.co"}})

	require.Error(t, err)
	assertStatus(t, err, http.StatusNotFound)
}

func TestLinkService_QRCodePNG(t *testing.T) {
	svc, links, ctx := newLinkService(t)
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://x.co"), nil)

	png, err := svc.QRCodePNG(ctx, 3, 7, 9)

	require.NoError(t, err)
	assert.True(t, bytes.HasPrefix(png, []byte("\x89PNG")), "should be a PNG image")
}

func TestRandomCode(t *testing.T) {
	seen := map[string]bool{}

	for range 50 {
		code, err := randomCode(7)
		require.NoError(t, err)
		assert.True(t, codeRe.MatchString(code), "code %q", code)

		seen[code] = true
	}

	assert.Len(t, seen, 50, "codes should not repeat")
}

// assertStatus asserts an error carries the wanted HTTP status code.
func assertStatus(t *testing.T, err error, want int) {
	t.Helper()

	sc, ok := err.(interface{ StatusCode() int })
	require.True(t, ok, "error %v should carry a status code", err)
	assert.Equal(t, want, sc.StatusCode())
}

// ---------------------------------------------------------------------------
// Destination editing (PATCH /api/v1/links/{id})
// ---------------------------------------------------------------------------

const (
	oldDest = "https://old.example.com/page"
	newDest = "https://new.example.com/landing"
)

// expectApplied wires the store calls of one successful destination edit by
// actorID: the UPDATE, the audit INSERT (asserting old/new URLs) and the
// reload behind the returned link.
func expectApplied(links *MockLinkStore, actorID int64, wantDest string) {
	links.EXPECT().UpdateDestination(gomock.Any(), int64(3), int64(9),
		wantDest, gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
	links.EXPECT().InsertEdit(gomock.Any(), &models.LinkEdit{
		OrgID: 3, LinkID: 9, UserID: actorID, OldURL: oldDest, NewURL: wantDest,
	}).Return(nil)

	updated := publicLink(9, "abc1234", wantDest)
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(updated, nil)
}

func TestLinkService_UpdateDestination(t *testing.T) {
	tests := []struct {
		desc       string
		actorID    int64
		input      EditInput
		setup      func(links *MockLinkStore, members *MockMemberStore)
		wantDest   string
		wantStatus int
	}{
		{
			desc:    "creator edits: normalized URL applied, audit row written, no role lookup",
			actorID: 7,
			input:   EditInput{URL: "new.example.com/landing"},
			setup: func(links *MockLinkStore, _ *MockMemberStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", oldDest), nil)
				expectApplied(links, 7, newDest)
			},
			wantDest: newDest,
		},
		{
			desc:    "non-creator org OWNER may edit (DB role)",
			actorID: 55,
			input:   EditInput{URL: newDest},
			setup: func(links *MockLinkStore, members *MockMemberStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", oldDest), nil)
				members.EXPECT().GetRole(gomock.Any(), int64(3), int64(55)).
					Return(models.RoleOwner, nil)
				expectApplied(links, 55, newDest)
			},
			wantDest: newDest,
		},
		{
			desc:    "API-key-created link (no creator): OWNER may edit",
			actorID: 55,
			input:   EditInput{URL: newDest},
			setup: func(links *MockLinkStore, members *MockMemberStore) {
				viaAPI := publicLink(9, "abc1234", oldDest)
				viaAPI.UserID = nil
				viaAPI.APIKeyID = ptr64(2)
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(viaAPI, nil)
				members.EXPECT().GetRole(gomock.Any(), int64(3), int64(55)).
					Return(models.RoleOwner, nil)
				expectApplied(links, 55, newDest)
			},
			wantDest: newDest,
		},
		{
			desc:    "non-creator MEMBER is 403",
			actorID: 55,
			input:   EditInput{URL: newDest},
			setup: func(links *MockLinkStore, members *MockMemberStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", oldDest), nil)
				members.EXPECT().GetRole(gomock.Any(), int64(3), int64(55)).
					Return(models.RoleMember, nil)
			},
			wantStatus: http.StatusForbidden,
		},
		{
			desc:    "non-member (no role row) is 403",
			actorID: 55,
			input:   EditInput{URL: newDest},
			setup: func(links *MockLinkStore, members *MockMemberStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", oldDest), nil)
				members.EXPECT().GetRole(gomock.Any(), int64(3), int64(55)).Return("", nil)
			},
			wantStatus: http.StatusForbidden,
		},
		{
			desc:    "cross-org / absent link is 404",
			actorID: 7,
			input:   EditInput{URL: newDest},
			setup: func(links *MockLinkStore, _ *MockMemberStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(nil, nil)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			desc:    "another user's PRIVATE link is 404 even for an OWNER (visibility first)",
			actorID: 55,
			input:   EditInput{URL: newDest},
			setup: func(links *MockLinkStore, _ *MockMemberStore) {
				private := publicLink(9, "abc1234", oldDest)
				private.Type = models.LinkTypePrivate
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(private, nil)
			},
			wantStatus: http.StatusNotFound,
		},
		{
			desc:    "malformed URL is 422, nothing written",
			actorID: 7,
			input:   EditInput{URL: "https:/broken"},
			setup: func(links *MockLinkStore, _ *MockMemberStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", oldDest), nil)
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			desc:    "own short domain rejected with 422",
			actorID: 7,
			input:   EditInput{URL: "http://sho.rt/other12"},
			setup: func(links *MockLinkStore, _ *MockMemberStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", oldDest), nil)
			},
			wantStatus: http.StatusUnprocessableEntity,
		},
		{
			desc:    "no-op edit (same destination) writes nothing, audits nothing",
			actorID: 7,
			input:   EditInput{URL: oldDest},
			setup: func(links *MockLinkStore, _ *MockMemberStore) {
				links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
					Return(publicLink(9, "abc1234", oldDest), nil)
			},
			wantDest: oldDest,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, links, members, ctx := newLinkServiceWithMembers(t)
			tc.setup(links, members)

			got, err := svc.UpdateDestination(ctx, 3, tc.actorID, 9, tc.input)

			if tc.wantStatus != 0 {
				require.Error(t, err)
				assertStatus(t, err, tc.wantStatus)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantDest, got.DestinationURL)
			assert.Equal(t, "http://sho.rt/abc1234", got.ShortURL)
		})
	}
}

func TestLinkService_UpdateDestination_BakesUTMsAndStoresThem(t *testing.T) {
	svc, links, _, ctx := newLinkServiceWithMembers(t)

	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", oldDest), nil)

	wantDest := "https://new.example.com/landing?utm_medium=email&utm_source=news+letter"

	links.EXPECT().UpdateDestination(gomock.Any(), int64(3), int64(9), wantDest,
		gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, _, _ int64, _ string, source, medium, campaign *string) error {
			require.NotNil(t, source)
			assert.Equal(t, "news letter", *source)
			require.NotNil(t, medium)
			assert.Equal(t, "email", *medium)
			assert.Nil(t, campaign)

			return nil
		})
	links.EXPECT().InsertEdit(gomock.Any(), gomock.Any()).Return(nil)
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", wantDest), nil)

	got, err := svc.UpdateDestination(ctx, 3, 7, 9,
		EditInput{URL: "new.example.com/landing", UTMSource: "news letter", UTMMedium: "email"})

	require.NoError(t, err)
	assert.Equal(t, wantDest, got.DestinationURL)
}

func TestLinkService_UpdateDestination_StoreErrorsPropagate(t *testing.T) {
	t.Run("update fails", func(t *testing.T) {
		svc, links, _, ctx := newLinkServiceWithMembers(t)

		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", oldDest), nil)
		links.EXPECT().UpdateDestination(gomock.Any(), int64(3), int64(9), newDest,
			gomock.Any(), gomock.Any(), gomock.Any()).Return(assert.AnError)

		_, err := svc.UpdateDestination(ctx, 3, 7, 9, EditInput{URL: newDest})
		require.Error(t, err)
	})

	t.Run("audit insert fails", func(t *testing.T) {
		svc, links, _, ctx := newLinkServiceWithMembers(t)

		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", oldDest), nil)
		links.EXPECT().UpdateDestination(gomock.Any(), int64(3), int64(9), newDest,
			gomock.Any(), gomock.Any(), gomock.Any()).Return(nil)
		links.EXPECT().InsertEdit(gomock.Any(), gomock.Any()).Return(assert.AnError)

		_, err := svc.UpdateDestination(ctx, 3, 7, 9, EditInput{URL: newDest})
		require.Error(t, err)
	})
}

// --- Short-URL display under a primary custom domain (FEATURES.md §1.6) -----

func TestLinkService_ShortURLDisplay(t *testing.T) {
	t.Run("primary verified domain wins, always https", func(t *testing.T) {
		svc, links, _, domains, ctx := newLinkServiceWithDomains(t)
		domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("links.example.com", nil)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://example.com"), nil)

		got, err := svc.Get(ctx, 3, 7, 9)

		require.NoError(t, err)
		// The service was built with scheme "http": custom domains still
		// display https (TLS for them is an operator concern).
		assert.Equal(t, "https://links.example.com/abc1234", got.ShortURL)
	})

	t.Run("no primary domain keeps the deployment base", func(t *testing.T) {
		svc, links, _, domains, ctx := newLinkServiceWithDomains(t)
		domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("", nil)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://example.com"), nil)

		got, err := svc.Get(ctx, 3, 7, 9)

		require.NoError(t, err)
		assert.Equal(t, "http://sho.rt/abc1234", got.ShortURL)
	})

	t.Run("primary lookup failure falls back, never blocks", func(t *testing.T) {
		svc, links, _, domains, ctx := newLinkServiceWithDomains(t)
		domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("", assert.AnError)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://example.com"), nil)

		got, err := svc.Get(ctx, 3, 7, 9)

		require.NoError(t, err)
		assert.Equal(t, "http://sho.rt/abc1234", got.ShortURL)
	})

	t.Run("list resolves the primary domain once per page", func(t *testing.T) {
		svc, links, _, domains, ctx := newLinkServiceWithDomains(t)
		// Times(1) is the default — two links, one domain lookup.
		domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("links.example.com", nil)
		links.EXPECT().List(gomock.Any(), int64(3), int64(7), 10, 0).
			Return([]models.Link{*publicLink(9, "abc1234", "https://a.example.com"),
				*publicLink(10, "zzz9876", "https://b.example.com")}, nil)
		links.EXPECT().Count(gomock.Any(), int64(3), int64(7)).Return(int64(2), nil)

		page, err := svc.List(ctx, 3, 7, 1)

		require.NoError(t, err)
		require.Len(t, page.Links, 2)
		assert.Equal(t, "https://links.example.com/abc1234", page.Links[0].ShortURL)
		assert.Equal(t, "https://links.example.com/zzz9876", page.Links[1].ShortURL)
	})

	t.Run("QR encodes the primary-domain short URL", func(t *testing.T) {
		svc, links, _, domains, ctx := newLinkServiceWithDomains(t)
		domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("links.example.com", nil)
		links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://example.com"), nil)

		withDomain, err := svc.QRCodePNG(ctx, 3, 7, 9)
		require.NoError(t, err)

		svc2, links2, _, domains2, ctx2 := newLinkServiceWithDomains(t)
		domains2.EXPECT().PrimaryVerifiedHostname(gomock.Any(), int64(3)).Return("", nil)
		links2.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
			Return(publicLink(9, "abc1234", "https://example.com"), nil)

		withoutDomain, err := svc2.QRCodePNG(ctx2, 3, 7, 9)
		require.NoError(t, err)

		assert.False(t, bytes.Equal(withDomain, withoutDomain),
			"the QR must encode the primary-domain URL, so the PNGs differ")
	})
}
