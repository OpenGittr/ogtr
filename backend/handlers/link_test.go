package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr/http/response"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
)

func newLinkHandler(t *testing.T) (*LinkHandler, *MockLinkService) {
	t.Helper()

	h, svc, _ := newLinkHandlerWithKeys(t)

	return h, svc
}

func newLinkHandlerWithKeys(t *testing.T) (*LinkHandler, *MockLinkService, *MockAPIKeyService) {
	t.Helper()

	ctrl := gomock.NewController(t)
	svc := NewMockLinkService(ctrl)
	keys := NewMockAPIKeyService(ctrl)

	return NewLinkHandler(svc, keys, nil), svc, keys
}

func TestLinkHandler_Shorten(t *testing.T) {
	link := &models.Link{ID: 9, Code: "abc1234", ShortURL: "http://sho.rt/abc1234"}

	tests := []struct {
		desc    string
		body    string
		orgless bool
		setup   func(svc *MockLinkService)
		wantErr bool
	}{
		{
			desc: "shortens with all fields",
			body: `{"url":"example.com","type":"PRIVATE","utm_source":"tw","utm_medium":"m","utm_campaign":"c"}`,
			setup: func(svc *MockLinkService) {
				svc.EXPECT().Shorten(gomock.Any(), int64(3), int64(7), services.ShortenInput{
					URL: "example.com", Type: "PRIVATE",
					UTMSource: "tw", UTMMedium: "m", UTMCampaign: "c",
				}).Return(link, nil)
			},
		},
		{desc: "missing url", body: `{}`, wantErr: true},
		{desc: "invalid body", body: `{`, wantErr: true},
		{desc: "org-less token rejected", body: `{"url":"example.com"}`, orgless: true, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newLinkHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			claims := orgOwnerClaims()
			if tc.orgless {
				claims = orglessClaims()
			}

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/links", tc.body, claims, nil)

			got, err := h.Shorten(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, link, got)
		})
	}
}

func TestLinkHandler_Shorten_WithAPIKey(t *testing.T) {
	const rawKey = "slk_0123456789012345678901234567890123456789"

	link := &models.Link{ID: 9, Code: "abc1234", ShortURL: "http://sho.rt/abc1234"}
	key := &models.APIKey{ID: 11, OrgID: 42, Name: "ci key", Status: models.APIKeyStatusEnabled}

	tests := []struct {
		desc    string
		setup   func(links *MockLinkService, keys *MockAPIKeyService)
		wantErr bool
	}{
		{
			desc: "valid key shortens into the key's org, not a token org",
			setup: func(links *MockLinkService, keys *MockAPIKeyService) {
				keys.EXPECT().Authenticate(gomock.Any(), rawKey).Return(key, nil)
				links.EXPECT().ShortenViaAPIKey(gomock.Any(), int64(42), int64(11),
					services.ShortenInput{URL: "example.com"}).Return(link, nil)
			},
		},
		{
			desc: "invalid key is rejected before any link work",
			setup: func(_ *MockLinkService, keys *MockAPIKeyService) {
				keys.EXPECT().Authenticate(gomock.Any(), rawKey).
					Return(nil, apierrors.Unauthorized("invalid API key"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, links, keys := newLinkHandlerWithKeys(t)
			tc.setup(links, keys)

			// No session claims: key auth carries the org context by itself.
			ctx := newTestCtx(t, http.MethodPost, "/api/v1/links", `{"url":"example.com"}`, nil, nil)
			ctx.Context = auth.ContextWithAPIKey(ctx.Context, rawKey)

			got, err := h.Shorten(ctx)

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, link, got)
		})
	}
}

func TestLinkHandler_List(t *testing.T) {
	page := &services.LinkPage{Page: 2, PerPage: 10, Total: 11, Links: []models.Link{}}

	tests := []struct {
		desc    string
		query   string
		setup   func(svc *MockLinkService)
		wantErr bool
	}{
		{
			desc:  "explicit page",
			query: "?page=2",
			setup: func(svc *MockLinkService) {
				svc.EXPECT().List(gomock.Any(), int64(3), int64(7), 2).Return(page, nil)
			},
		},
		{
			desc: "default page 1",
			setup: func(svc *MockLinkService) {
				svc.EXPECT().List(gomock.Any(), int64(3), int64(7), 1).Return(page, nil)
			},
		},
		{desc: "garbage page", query: "?page=x", wantErr: true},
		{desc: "zero page", query: "?page=0", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newLinkHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/links"+tc.query, "", orgOwnerClaims(), nil)

			got, err := h.List(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, page, got)
		})
	}
}

func TestLinkHandler_Get(t *testing.T) {
	link := &models.Link{ID: 9, Code: "abc1234"}

	tests := []struct {
		desc    string
		id      string
		setup   func(svc *MockLinkService)
		wantErr bool
	}{
		{
			desc: "found",
			id:   "9",
			setup: func(svc *MockLinkService) {
				svc.EXPECT().Get(gomock.Any(), int64(3), int64(7), int64(9)).Return(link, nil)
			},
		},
		{desc: "bad id", id: "x", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newLinkHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/links/"+tc.id, "", orgOwnerClaims(),
				map[string]string{"id": tc.id})

			got, err := h.Get(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, link, got)
		})
	}
}

func TestLinkHandler_SetAlias(t *testing.T) {
	link := &models.Link{ID: 9, Code: "my-brand"}

	tests := []struct {
		desc    string
		body    string
		setup   func(svc *MockLinkService)
		wantErr bool
	}{
		{
			desc: "alias set",
			body: `{"alias":"my-brand"}`,
			setup: func(svc *MockLinkService) {
				svc.EXPECT().SetAlias(gomock.Any(), int64(3), int64(7), int64(9), "my-brand").Return(link, nil)
			},
		},
		{desc: "missing alias", body: `{}`, wantErr: true},
		{desc: "invalid body", body: `{`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newLinkHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			ctx := newTestCtx(t, http.MethodPut, "/api/v1/links/9/alias", tc.body, orgOwnerClaims(),
				map[string]string{"id": "9"})

			got, err := h.SetAlias(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, link, got)
		})
	}
}

func TestLinkHandler_SetDeeplink(t *testing.T) {
	link := &models.Link{ID: 9, Code: "abc1234"}

	android := &models.AndroidDeeplink{
		Intent: "open", Package: "com.x", Scheme: "x", FallbackURL: "https://x.co",
	}

	tests := []struct {
		desc    string
		body    string
		want    *models.DeeplinkConfig
		wantErr bool
	}{
		{
			desc: "android config",
			body: `{"android":{"intent":"open","package":"com.x","scheme":"x","fallback_url":"https://x.co"}}`,
			want: &models.DeeplinkConfig{Android: android},
		},
		{
			desc: "empty body clears",
			body: "",
			want: &models.DeeplinkConfig{},
		},
		{
			desc: "null body clears",
			body: "null",
			want: &models.DeeplinkConfig{},
		},
		{
			desc: "empty object clears",
			body: "{}",
			want: &models.DeeplinkConfig{},
		},
		{desc: "invalid body", body: `{"android":`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newLinkHandler(t)

			if !tc.wantErr {
				svc.EXPECT().SetDeeplink(gomock.Any(), int64(3), int64(7), int64(9), tc.want).Return(link, nil)
			}

			ctx := newTestCtx(t, http.MethodPut, "/api/v1/links/9/deeplink", tc.body, orgOwnerClaims(),
				map[string]string{"id": "9"})

			got, err := h.SetDeeplink(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, link, got)
		})
	}
}

func TestLinkHandler_SetDeeplink_BadID(t *testing.T) {
	h, _ := newLinkHandler(t)

	ctx := newTestCtx(t, http.MethodPut, "/api/v1/links/x/deeplink", "{}", orgOwnerClaims(),
		map[string]string{"id": "x"})

	_, err := h.SetDeeplink(ctx)
	require.Error(t, err)
}

func TestLinkHandler_QR(t *testing.T) {
	h, svc := newLinkHandler(t)
	svc.EXPECT().QRCodePNG(gomock.Any(), int64(3), int64(7), int64(9)).Return([]byte("png-bytes"), nil)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/links/9/qr", "", orgOwnerClaims(),
		map[string]string{"id": "9"})

	got, err := h.QR(ctx)

	require.NoError(t, err)
	file, ok := got.(response.File)
	require.True(t, ok, "QR should be a raw file response")
	assert.Equal(t, "image/png", file.ContentType)
	assert.Equal(t, []byte("png-bytes"), file.Content)
}

func TestLinkHandler_QR_Error(t *testing.T) {
	h, svc := newLinkHandler(t)
	svc.EXPECT().QRCodePNG(gomock.Any(), int64(3), int64(7), int64(9)).Return(nil, assert.AnError)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/links/9/qr", "", orgOwnerClaims(),
		map[string]string{"id": "9"})

	_, err := h.QR(ctx)
	require.Error(t, err)
}

func TestLinkHandler_UpdateDestination(t *testing.T) {
	link := &models.Link{ID: 9, Code: "abc1234", DestinationURL: "https://new.example.com",
		ShortURL: "http://sho.rt/abc1234"}

	tests := []struct {
		desc    string
		body    string
		id      string
		orgless bool
		setup   func(svc *MockLinkService)
		wantErr bool
	}{
		{
			desc: "edits destination with UTM fields",
			body: `{"url":"new.example.com","utm_source":"tw","utm_medium":"m","utm_campaign":"c"}`,
			id:   "9",
			setup: func(svc *MockLinkService) {
				svc.EXPECT().UpdateDestination(gomock.Any(), int64(3), int64(7), int64(9),
					services.EditInput{URL: "new.example.com", UTMSource: "tw", UTMMedium: "m", UTMCampaign: "c"}).
					Return(link, nil)
			},
		},
		{desc: "missing url", body: `{}`, id: "9", wantErr: true},
		{desc: "invalid body", body: `{`, id: "9", wantErr: true},
		{desc: "bad id", body: `{"url":"new.example.com"}`, id: "abc", wantErr: true},
		{desc: "org-less token rejected", body: `{"url":"new.example.com"}`, id: "9", orgless: true, wantErr: true},
		{
			desc: "service error returns untyped nil (gofr 206 regression)",
			body: `{"url":"new.example.com"}`,
			id:   "9",
			setup: func(svc *MockLinkService) {
				svc.EXPECT().UpdateDestination(gomock.Any(), int64(3), int64(7), int64(9),
					services.EditInput{URL: "new.example.com"}).
					Return(nil, apierrors.Forbidden("only the link's creator or an org owner can edit its destination"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newLinkHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			claims := orgOwnerClaims()
			if tc.orgless {
				claims = orglessClaims()
			}

			ctx := newTestCtx(t, http.MethodPatch, "/api/v1/links/"+tc.id, tc.body, claims,
				map[string]string{"id": tc.id})

			got, err := h.UpdateDestination(ctx)

			if tc.wantErr {
				require.Error(t, err)
				// Strictly untyped nil: a typed nil *models.Link in the any
				// return makes gofr respond 206 instead of the error status.
				assert.True(t, got == nil, "expected untyped nil, got %#v", got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, link, got)
		})
	}
}
