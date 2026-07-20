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
	"github.com/opengittr/ogtr/backend/visitor"
)

func newResolveHandler(t *testing.T) (*ResolveHandler, *MockResolveService) {
	t.Helper()

	h, svc, _ := newResolveHandlerWithKeys(t)

	return h, svc
}

func newResolveHandlerWithKeys(t *testing.T) (*ResolveHandler, *MockResolveService, *MockAPIKeyService) {
	t.Helper()

	ctrl := gomock.NewController(t)
	svc := NewMockResolveService(ctrl)
	keys := NewMockAPIKeyService(ctrl)

	return NewResolveHandler(svc, keys, "", "sho.rt", ""), svc, keys
}

func TestResolveHandler_Root(t *testing.T) {
	h, _, keys := newResolveHandlerWithKeys(t)
	ctx := newTestCtx(t, http.MethodGet, "/", "", nil, nil)

	// WEBSITE_URL unset: the bare domain stays a 404.
	_, err := h.Root(ctx)
	require.Error(t, err)

	var apiErr apierrors.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusNotFound, apiErr.StatusCode())

	// WEBSITE_URL set: 302 bounce to the website.
	h = NewResolveHandler(nil, keys, "https://www.example.com", "sho.rt", "")

	got, err := h.Root(ctx)
	require.NoError(t, err)
	redirect, ok := got.(response.Redirect)
	require.True(t, ok, "should be a redirect response")
	assert.Equal(t, "https://www.example.com", redirect.URL)
}

// TestResolveHandler_Root_CustomDomainNeverBounces pins the host rule: the
// WEBSITE_URL bounce belongs to the deployment's own short domain only — the
// bare root of a custom (or any unknown) domain is a 404 even with
// WEBSITE_URL configured.
func TestResolveHandler_Root_CustomDomainNeverBounces(t *testing.T) {
	_, _, keys := newResolveHandlerWithKeys(t)
	h := NewResolveHandler(nil, keys, "https://www.example.com", "sho.rt", "")

	tests := []struct {
		host       string
		wantBounce bool
	}{
		{"links.example.com", false},
		{"links.example.com:443", false},
		{"sho.rt", true},
		{"sho.rt:5810", true},
		{"localhost:5810", true},
		{"", true},
	}

	for _, tc := range tests {
		t.Run("host "+tc.host, func(t *testing.T) {
			ctx := newTestCtx(t, http.MethodGet, "/", "", nil, nil)
			ctx.Context = visitor.ContextWithEnv(ctx.Context, visitor.Env{Host: tc.host})

			got, err := h.Root(ctx)

			if !tc.wantBounce {
				require.Error(t, err)

				var apiErr apierrors.Error
				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, http.StatusNotFound, apiErr.StatusCode())

				return
			}

			require.NoError(t, err)
			redirect, ok := got.(response.Redirect)
			require.True(t, ok, "should be a redirect response")
			assert.Equal(t, "https://www.example.com", redirect.URL)
		})
	}
}

func TestResolveHandler_Redirect(t *testing.T) {
	h, svc := newResolveHandler(t)
	svc.EXPECT().Resolve(gomock.Any(), "abc1234", "", gomock.Any()).
		Return(&services.Resolution{Code: "abc1234", URL: "https://example.com?utm_source=ogtr"}, nil)

	ctx := newTestCtx(t, http.MethodGet, "/abc1234", "", nil, map[string]string{"code": "abc1234"})

	got, err := h.Redirect(ctx)

	require.NoError(t, err)
	redirect, ok := got.(response.Redirect)
	require.True(t, ok, "should be a redirect response")
	assert.Equal(t, "https://example.com?utm_source=ogtr", redirect.URL)
}

func TestResolveHandler_Redirect_UnknownCode(t *testing.T) {
	h, svc := newResolveHandler(t)
	svc.EXPECT().Resolve(gomock.Any(), "nope", "", gomock.Any()).
		Return(nil, apierrors.NotFound("short link not found"))

	ctx := newTestCtx(t, http.MethodGet, "/nope", "", nil, map[string]string{"code": "nope"})

	_, err := h.Redirect(ctx)

	require.Error(t, err)
	sc, ok := err.(interface{ StatusCode() int })
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, sc.StatusCode())
}

func TestResolveHandler_Redirect_PassesVisitorEnv(t *testing.T) {
	h, svc := newResolveHandler(t)

	env := visitor.Env{DeviceType: "Mobile", MobileOS: "iOS", Browser: "Safari", Referrer: "https://t.co/x", IP: "1.2.3.4"}

	svc.EXPECT().Resolve(gomock.Any(), "abc1234", "", env).
		Return(&services.Resolution{Code: "abc1234", URL: "https://example.com"}, nil)

	ctx := newTestCtx(t, http.MethodGet, "/abc1234", "", nil, map[string]string{"code": "abc1234"})
	ctx.Context = visitor.ContextWithEnv(ctx.Context, env)

	_, err := h.Redirect(ctx)
	require.NoError(t, err)
}

func TestResolveHandler_Resolve_WithAPIKey(t *testing.T) {
	const rawKey = "slk_0123456789012345678901234567890123456789"

	res := &services.Resolution{Code: "abc1234", URL: "https://example.com"}

	tests := []struct {
		desc    string
		setup   func(svc *MockResolveService, keys *MockAPIKeyService)
		wantErr bool
	}{
		{
			desc: "valid key resolves exactly like anonymous",
			setup: func(svc *MockResolveService, keys *MockAPIKeyService) {
				keys.EXPECT().Authenticate(gomock.Any(), rawKey).
					Return(&models.APIKey{ID: 11, OrgID: 42}, nil)
				svc.EXPECT().Resolve(gomock.Any(), "abc1234", "", gomock.Any()).Return(res, nil)
			},
		},
		{
			desc: "wrong key fails loudly instead of silently proceeding",
			setup: func(_ *MockResolveService, keys *MockAPIKeyService) {
				keys.EXPECT().Authenticate(gomock.Any(), rawKey).
					Return(nil, apierrors.Unauthorized("invalid API key"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc, keys := newResolveHandlerWithKeys(t)
			tc.setup(svc, keys)

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/resolve?code=abc1234", "", nil, nil)
			ctx.Context = auth.ContextWithAPIKey(ctx.Context, rawKey)

			got, err := h.Resolve(ctx)

			if tc.wantErr {
				require.Error(t, err)
				sc, ok := err.(interface{ StatusCode() int })
				require.True(t, ok)
				assert.Equal(t, http.StatusUnauthorized, sc.StatusCode())
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, res, got)
		})
	}
}

func TestResolveHandler_Resolve(t *testing.T) {
	res := &services.Resolution{Code: "abc1234", URL: "https://example.com"}

	tests := []struct {
		desc    string
		query   string
		setup   func(svc *MockResolveService)
		wantErr bool
	}{
		{
			desc:  "resolves with tag",
			query: "?code=abc1234&tag=campaign-42",
			setup: func(svc *MockResolveService) {
				svc.EXPECT().Resolve(gomock.Any(), "abc1234", "campaign-42", gomock.Any()).Return(res, nil)
			},
		},
		{desc: "missing code", query: "", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newResolveHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/resolve"+tc.query, "", nil, nil)

			got, err := h.Resolve(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, res, got)
		})
	}
}
