package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
)

// bothProviders builds an AuthHandler with google + dev enabled (the
// .local.env evaluation setup).
func bothProviders(authSvc AuthService) *AuthHandler {
	return NewAuthHandler(authSvc, []string{auth.ProviderGoogle, auth.ProviderDev}, "client-123")
}

func TestAuthHandler_GoogleLogin(t *testing.T) {
	result := &services.AuthResult{
		TokenPair:   models.TokenPair{AccessToken: "a", RefreshToken: "r"},
		User:        &models.User{ID: 7},
		Orgs:        []models.OrgMembership{},
		ActiveOrgID: 0,
	}

	tests := []struct {
		desc       string
		body       string
		providers  []string
		setup      func(m *MockAuthService)
		wantErr    bool
		wantStatus int
	}{
		{
			desc: "valid login",
			body: `{"id_token":"tok"}`,
			setup: func(m *MockAuthService) {
				m.EXPECT().Login(gomock.Any(), auth.ProviderGoogle, "tok").Return(result, nil)
			},
		},
		{
			desc:       "google disabled is a 404, body never read",
			body:       `{"id_token":"tok"}`,
			providers:  []string{auth.ProviderDev},
			setup:      func(*MockAuthService) {},
			wantErr:    true,
			wantStatus: http.StatusNotFound,
		},
		{
			desc:    "missing id_token",
			body:    `{}`,
			setup:   func(*MockAuthService) {},
			wantErr: true,
		},
		{
			desc:    "malformed body",
			body:    `{"id_token":`,
			setup:   func(*MockAuthService) {},
			wantErr: true,
		},
		{
			desc: "provider rejection propagates",
			body: `{"id_token":"bad"}`,
			setup: func(m *MockAuthService) {
				m.EXPECT().Login(gomock.Any(), auth.ProviderGoogle, "bad").
					Return(nil, apierrors.Unauthorized("invalid google id token"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			authSvc := NewMockAuthService(ctrl)
			tc.setup(authSvc)

			h := bothProviders(authSvc)
			if tc.providers != nil {
				h = NewAuthHandler(authSvc, tc.providers, "client-123")
			}

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/auth/google", tc.body, nil, nil)

			got, err := h.GoogleLogin(ctx)

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got, "error responses must carry nil data so Gofr uses the error status code")
				assertStatus(t, err, tc.wantStatus)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, result, got)
		})
	}
}

// assertStatus checks the error's StatusCode() when the test pins one.
func assertStatus(t *testing.T, err error, want int) {
	t.Helper()

	if want == 0 {
		return
	}

	var coded interface{ StatusCode() int }

	require.ErrorAs(t, err, &coded)
	assert.Equal(t, want, coded.StatusCode())
}

func TestAuthHandler_DevLogin(t *testing.T) {
	result := &services.AuthResult{
		TokenPair:   models.TokenPair{AccessToken: "a", RefreshToken: "r"},
		User:        &models.User{ID: 9},
		Orgs:        []models.OrgMembership{},
		ActiveOrgID: 0,
	}

	tests := []struct {
		desc       string
		body       string
		providers  []string
		setup      func(m *MockAuthService)
		wantErr    bool
		wantStatus int
	}{
		{
			desc: "valid dev login flows into the shared login path",
			body: `{"email":"eval@example.com","name":"Eval User"}`,
			setup: func(m *MockAuthService) {
				m.EXPECT().Login(gomock.Any(), auth.ProviderDev,
					auth.EncodeDevCredential("eval@example.com", "Eval User")).Return(result, nil)
			},
		},
		{
			// The disabled-mode proof: without "dev" in AUTH_PROVIDERS the
			// endpoint answers 404 and never touches the service.
			desc:       "dev disabled is a 404, service never called",
			body:       `{"email":"eval@example.com","name":"Eval User"}`,
			providers:  []string{auth.ProviderGoogle},
			setup:      func(*MockAuthService) {},
			wantErr:    true,
			wantStatus: http.StatusNotFound,
		},
		{
			desc:    "malformed body",
			body:    `{"email":`,
			setup:   func(*MockAuthService) {},
			wantErr: true,
		},
		{
			desc: "validation failure propagates as 422",
			body: `{"email":"not-an-email","name":"Eval User"}`,
			setup: func(m *MockAuthService) {
				m.EXPECT().Login(gomock.Any(), auth.ProviderDev,
					auth.EncodeDevCredential("not-an-email", "Eval User")).
					Return(nil, apierrors.Unprocessable("email is not a valid email address"))
			},
			wantErr:    true,
			wantStatus: http.StatusUnprocessableEntity,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			authSvc := NewMockAuthService(ctrl)
			tc.setup(authSvc)

			h := bothProviders(authSvc)
			if tc.providers != nil {
				h = NewAuthHandler(authSvc, tc.providers, "client-123")
			}

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/auth/dev", tc.body, nil, nil)

			got, err := h.DevLogin(ctx)

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got, "error responses must carry nil data so Gofr uses the error status code")
				assertStatus(t, err, tc.wantStatus)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, result, got)
		})
	}
}

func TestAuthHandler_Providers(t *testing.T) {
	tests := []struct {
		desc      string
		providers []string
		clientID  string
		want      ProvidersResponse
	}{
		{
			desc:      "google and dev with a client id",
			providers: []string{auth.ProviderGoogle, auth.ProviderDev},
			clientID:  "client-123",
			want:      ProvidersResponse{Providers: []string{"google", "dev"}, GoogleClientID: "client-123"},
		},
		{
			desc:      "google only",
			providers: []string{auth.ProviderGoogle},
			clientID:  "client-123",
			want:      ProvidersResponse{Providers: []string{"google"}, GoogleClientID: "client-123"},
		},
		{
			desc:      "dev only never leaks the client id",
			providers: []string{auth.ProviderDev},
			clientID:  "client-123",
			want:      ProvidersResponse{Providers: []string{"dev"}, GoogleClientID: ""},
		},
		{
			desc:      "google enabled but unconfigured client id stays empty",
			providers: []string{auth.ProviderGoogle},
			clientID:  "",
			want:      ProvidersResponse{Providers: []string{"google"}, GoogleClientID: ""},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			h := NewAuthHandler(NewMockAuthService(ctrl), tc.providers, tc.clientID)

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/auth/providers", "", nil, nil)

			got, err := h.Providers(ctx)

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestAuthHandler_Refresh(t *testing.T) {
	tests := []struct {
		desc    string
		body    string
		setup   func(m *MockAuthService)
		wantErr bool
	}{
		{
			desc: "valid refresh",
			body: `{"refresh_token":"ref"}`,
			setup: func(m *MockAuthService) {
				m.EXPECT().Refresh(gomock.Any(), "ref").
					Return(models.TokenPair{AccessToken: "a2", RefreshToken: "r2"}, nil)
			},
		},
		{
			desc:    "missing refresh_token",
			body:    `{}`,
			setup:   func(*MockAuthService) {},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			authSvc := NewMockAuthService(ctrl)
			tc.setup(authSvc)

			h := bothProviders(authSvc)
			ctx := newTestCtx(t, http.MethodPost, "/api/v1/auth/refresh", tc.body, nil, nil)

			got, err := h.Refresh(ctx)

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got, "error responses must carry nil data so Gofr uses the error status code")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, models.TokenPair{AccessToken: "a2", RefreshToken: "r2"}, got)
		})
	}
}

func TestAuthHandler_Me(t *testing.T) {
	tests := []struct {
		desc      string
		useClaims bool
		wantErr   bool
	}{
		{desc: "authenticated", useClaims: true},
		{desc: "no claims in context", wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			authSvc := NewMockAuthService(ctrl)

			var claims = orgOwnerClaims()

			me := &services.MeResult{User: &models.User{ID: 7}, ActiveOrgID: 3}

			if tc.useClaims {
				authSvc.EXPECT().Me(gomock.Any(), claims).Return(me, nil)
			}

			h := bothProviders(authSvc)

			var ctx = newTestCtx(t, http.MethodGet, "/api/v1/me", "", nil, nil)
			if tc.useClaims {
				ctx = newTestCtx(t, http.MethodGet, "/api/v1/me", "", claims, nil)
			}

			got, err := h.Me(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, me, got)
		})
	}
}

func TestAuthHandler_SwitchOrg(t *testing.T) {
	tests := []struct {
		desc    string
		body    string
		setup   func(m *MockAuthService)
		wantErr bool
	}{
		{
			desc: "valid switch",
			body: `{"org_id":5}`,
			setup: func(m *MockAuthService) {
				m.EXPECT().SwitchOrg(gomock.Any(), int64(7), int64(5)).
					Return(models.TokenPair{AccessToken: "a3", RefreshToken: "r3"}, nil)
			},
		},
		{
			desc:    "missing org_id",
			body:    `{}`,
			setup:   func(*MockAuthService) {},
			wantErr: true,
		},
		{
			desc: "not a member",
			body: `{"org_id":5}`,
			setup: func(m *MockAuthService) {
				m.EXPECT().SwitchOrg(gomock.Any(), int64(7), int64(5)).
					Return(models.TokenPair{}, apierrors.Forbidden("you are not a member of this org"))
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			authSvc := NewMockAuthService(ctrl)
			tc.setup(authSvc)

			h := bothProviders(authSvc)
			ctx := newTestCtx(t, http.MethodPost, "/api/v1/auth/switch-org", tc.body, orgOwnerClaims(), nil)

			got, err := h.SwitchOrg(ctx)

			if tc.wantErr {
				require.Error(t, err)
				assert.Nil(t, got, "error responses must carry nil data so Gofr uses the error status code")

				return
			}

			require.NoError(t, err)
			assert.Equal(t, models.TokenPair{AccessToken: "a3", RefreshToken: "r3"}, got)
		})
	}
}
