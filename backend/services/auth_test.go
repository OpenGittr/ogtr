package services

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/models"
)

var errStore = errors.New("store blew up")

type authMocks struct {
	provider *auth.MockIdentityProvider
	tokens   *MockTokenIssuer
	users    *MockUserStore
	orgs     *MockOrgStore
	members  *MockMemberStore
	invites  *MockInviteStore
}

func newAuthService(t *testing.T) (*AuthService, authMocks, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	m := authMocks{
		provider: auth.NewMockIdentityProvider(ctrl),
		tokens:   NewMockTokenIssuer(ctrl),
		users:    NewMockUserStore(ctrl),
		orgs:     NewMockOrgStore(ctrl),
		members:  NewMockMemberStore(ctrl),
		invites:  NewMockInviteStore(ctrl),
	}

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	// The mock provider is registered as "google"; a real DevProvider sits
	// behind "dev" so the dev login flow is exercised end-to-end in tests.
	providers := map[string]auth.IdentityProvider{
		auth.ProviderGoogle: m.provider,
		auth.ProviderDev:    auth.NewDevProvider(),
	}

	return NewAuthService(providers, m.tokens, m.users, m.orgs, m.members, m.invites), m, ctx
}

func pairFor(userID, orgID int64) models.TokenPair {
	return models.TokenPair{AccessToken: "acc", RefreshToken: "ref"}
}

func TestAuthService_Login(t *testing.T) {
	identity := auth.Identity{Email: "Alice@Acme.com", Name: "Alice"}
	user := &models.User{ID: 7, Name: "Alice", Email: "alice@acme.com", Status: models.UserStatusEnabled}
	org := &models.Org{ID: 3, Name: "Acme", Slug: "acme"}
	membership := models.OrgMembership{OrgID: 3, Name: "Acme", Slug: "acme", Role: models.RoleOwner}

	tests := []struct {
		desc        string
		setup       func(m authMocks)
		wantErr     error
		wantOrgs    int
		wantActive  int64
	}{
		{
			desc: "existing user with membership",
			setup: func(m authMocks) {
				m.provider.EXPECT().Verify(gomock.Any(), "cred").Return(identity, nil)
				m.users.EXPECT().GetByEmail(gomock.Any(), "alice@acme.com").Return(user, nil)
				m.invites.EXPECT().PendingForEmail(gomock.Any(), "alice@acme.com").Return([]models.Invite{}, nil)
				m.members.EXPECT().ListOrgsForUser(gomock.Any(), int64(7)).Return([]models.OrgMembership{membership}, nil)
				m.tokens.EXPECT().IssuePair(int64(7), int64(3), models.RoleOwner).Return(pairFor(7, 3), nil)
			},
			wantOrgs:   1,
			wantActive: 3,
		},
		{
			desc: "JIT-created user with no org",
			setup: func(m authMocks) {
				m.provider.EXPECT().Verify(gomock.Any(), "cred").Return(identity, nil)
				m.users.EXPECT().GetByEmail(gomock.Any(), "alice@acme.com").Return(nil, nil)
				m.users.EXPECT().Create(gomock.Any(), "Alice", "alice@acme.com").Return(user, nil)
				m.invites.EXPECT().PendingForEmail(gomock.Any(), "alice@acme.com").Return([]models.Invite{}, nil)
				m.members.EXPECT().ListOrgsForUser(gomock.Any(), int64(7)).Return([]models.OrgMembership{}, nil)
				m.orgs.EXPECT().GetByAutoJoinDomain(gomock.Any(), "acme.com").Return(nil, nil)
				m.tokens.EXPECT().IssuePair(int64(7), int64(0), "").Return(pairFor(7, 0), nil)
			},
			wantOrgs:   0,
			wantActive: 0,
		},
		{
			desc: "org-less user auto-joins by email domain as MEMBER",
			setup: func(m authMocks) {
				m.provider.EXPECT().Verify(gomock.Any(), "cred").Return(identity, nil)
				m.users.EXPECT().GetByEmail(gomock.Any(), "alice@acme.com").Return(user, nil)
				m.invites.EXPECT().PendingForEmail(gomock.Any(), "alice@acme.com").Return([]models.Invite{}, nil)
				m.members.EXPECT().ListOrgsForUser(gomock.Any(), int64(7)).Return([]models.OrgMembership{}, nil)
				m.orgs.EXPECT().GetByAutoJoinDomain(gomock.Any(), "acme.com").Return(org, nil)
				m.members.EXPECT().Add(gomock.Any(), int64(3), int64(7), models.RoleMember).Return(nil)
				m.tokens.EXPECT().IssuePair(int64(7), int64(3), models.RoleMember).Return(pairFor(7, 3), nil)
			},
			wantOrgs:   1,
			wantActive: 3,
		},
		{
			desc: "pending invite converts to membership on login",
			setup: func(m authMocks) {
				invite := models.Invite{ID: 12, OrgID: 3, Email: "alice@acme.com", Status: models.InviteStatusPending}
				m.provider.EXPECT().Verify(gomock.Any(), "cred").Return(identity, nil)
				m.users.EXPECT().GetByEmail(gomock.Any(), "alice@acme.com").Return(user, nil)
				m.invites.EXPECT().PendingForEmail(gomock.Any(), "alice@acme.com").Return([]models.Invite{invite}, nil)
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return("", nil)
				m.members.EXPECT().Add(gomock.Any(), int64(3), int64(7), models.RoleMember).Return(nil)
				m.invites.EXPECT().SetStatus(gomock.Any(), int64(12), models.InviteStatusAccepted).Return(nil)
				m.members.EXPECT().ListOrgsForUser(gomock.Any(), int64(7)).
					Return([]models.OrgMembership{{OrgID: 3, Name: "Acme", Slug: "acme", Role: models.RoleMember}}, nil)
				m.tokens.EXPECT().IssuePair(int64(7), int64(3), models.RoleMember).Return(pairFor(7, 3), nil)
			},
			wantOrgs:   1,
			wantActive: 3,
		},
		{
			desc: "provider rejection propagates",
			setup: func(m authMocks) {
				m.provider.EXPECT().Verify(gomock.Any(), "cred").
					Return(auth.Identity{}, apierrors.Unauthorized("invalid google id token"))
			},
			wantErr: apierrors.Unauthorized("invalid google id token"),
		},
		{
			desc: "invalid email from provider",
			setup: func(m authMocks) {
				m.provider.EXPECT().Verify(gomock.Any(), "cred").Return(auth.Identity{Email: "not-an-email"}, nil)
			},
			wantErr: apierrors.Unauthorized("identity provider returned an invalid email"),
		},
		{
			desc: "store failure propagates",
			setup: func(m authMocks) {
				m.provider.EXPECT().Verify(gomock.Any(), "cred").Return(identity, nil)
				m.users.EXPECT().GetByEmail(gomock.Any(), "alice@acme.com").Return(nil, errStore)
			},
			wantErr: errStore,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newAuthService(t)
			tc.setup(m)

			result, err := svc.Login(ctx, auth.ProviderGoogle, "cred")

			if tc.wantErr != nil {
				require.Error(t, err)
				assert.Equal(t, tc.wantErr, err)

				return
			}

			require.NoError(t, err)
			assert.Len(t, result.Orgs, tc.wantOrgs)
			assert.Equal(t, tc.wantActive, result.ActiveOrgID)
			assert.NotEmpty(t, result.AccessToken)
			assert.NotEmpty(t, result.RefreshToken)
		})
	}
}

func TestAuthService_Login_UnknownProviderIs404(t *testing.T) {
	svc, _, ctx := newAuthService(t)

	result, err := svc.Login(ctx, "okta", "cred")

	assert.Equal(t, apierrors.NotFound("this sign-in method is not enabled on this server"), err)
	assert.Nil(t, result)
}

// TestAuthService_Login_DevProvider drives the real DevProvider through the
// shared login path: same JIT user creation, invite conversion and org
// resolution as Google — nothing dev-specific after Verify.
func TestAuthService_Login_DevProvider(t *testing.T) {
	user := &models.User{ID: 9, Name: "Eval User", Email: "eval@example.com", Status: models.UserStatusEnabled}

	tests := []struct {
		desc       string
		credential string
		setup      func(m authMocks)
		wantErr    error
	}{
		{
			desc:       "JIT-creates the user and issues a pair",
			credential: auth.EncodeDevCredential("Eval@Example.com", "Eval User"),
			setup: func(m authMocks) {
				m.users.EXPECT().GetByEmail(gomock.Any(), "eval@example.com").Return(nil, nil)
				m.users.EXPECT().Create(gomock.Any(), "Eval User", "eval@example.com").Return(user, nil)
				m.invites.EXPECT().PendingForEmail(gomock.Any(), "eval@example.com").Return([]models.Invite{}, nil)
				m.members.EXPECT().ListOrgsForUser(gomock.Any(), int64(9)).Return([]models.OrgMembership{}, nil)
				m.orgs.EXPECT().GetByAutoJoinDomain(gomock.Any(), "example.com").Return(nil, nil)
				m.tokens.EXPECT().IssuePair(int64(9), int64(0), "").Return(pairFor(9, 0), nil)
			},
		},
		{
			desc:       "invalid email is a 422 before any store call",
			credential: auth.EncodeDevCredential("not-an-email", "Eval User"),
			setup:      func(authMocks) {},
			wantErr:    apierrors.Unprocessable("email is not a valid email address"),
		},
		{
			desc:       "empty name is a 422 before any store call",
			credential: auth.EncodeDevCredential("eval@example.com", "  "),
			setup:      func(authMocks) {},
			wantErr:    apierrors.Unprocessable("name must not be empty"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newAuthService(t)
			tc.setup(m)

			result, err := svc.Login(ctx, auth.ProviderDev, tc.credential)

			if tc.wantErr != nil {
				assert.Equal(t, tc.wantErr, err)
				assert.Nil(t, result)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, user, result.User)
			assert.Empty(t, result.Orgs)
			assert.Zero(t, result.ActiveOrgID)
			assert.NotEmpty(t, result.AccessToken)
		})
	}
}

func TestAuthService_Refresh(t *testing.T) {
	user := &models.User{ID: 7, Status: models.UserStatusEnabled}
	refreshClaims := &auth.SessionClaims{UserID: 7, OrgID: 3, Role: models.RoleOwner, TokenType: auth.TokenTypeRefresh}

	tests := []struct {
		desc    string
		setup   func(m authMocks)
		wantErr bool
	}{
		{
			desc: "valid refresh keeps org scope with current role",
			setup: func(m authMocks) {
				m.tokens.EXPECT().Parse("ref", auth.TokenTypeRefresh).Return(refreshClaims, nil)
				m.users.EXPECT().GetByID(gomock.Any(), int64(7)).Return(user, nil)
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return(models.RoleMember, nil)
				m.tokens.EXPECT().IssuePair(int64(7), int64(3), models.RoleMember).Return(pairFor(7, 3), nil)
			},
		},
		{
			desc: "membership revoked since issue falls back to org-less pair",
			setup: func(m authMocks) {
				m.tokens.EXPECT().Parse("ref", auth.TokenTypeRefresh).Return(refreshClaims, nil)
				m.users.EXPECT().GetByID(gomock.Any(), int64(7)).Return(user, nil)
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return("", nil)
				m.tokens.EXPECT().IssuePair(int64(7), int64(0), "").Return(pairFor(7, 0), nil)
			},
		},
		{
			desc: "invalid refresh token",
			setup: func(m authMocks) {
				m.tokens.EXPECT().Parse("ref", auth.TokenTypeRefresh).
					Return(nil, apierrors.Unauthorized("invalid or expired token"))
			},
			wantErr: true,
		},
		{
			desc: "deleted user is rejected",
			setup: func(m authMocks) {
				m.tokens.EXPECT().Parse("ref", auth.TokenTypeRefresh).Return(refreshClaims, nil)
				m.users.EXPECT().GetByID(gomock.Any(), int64(7)).Return(nil, nil)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newAuthService(t)
			tc.setup(m)

			pair, err := svc.Refresh(ctx, "ref")

			if tc.wantErr {
				require.Error(t, err)
				assert.Empty(t, pair.AccessToken)

				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, pair.AccessToken)
		})
	}
}

func TestAuthService_Me(t *testing.T) {
	svc, m, ctx := newAuthService(t)

	user := &models.User{ID: 7, Name: "Alice"}
	m.users.EXPECT().GetByID(gomock.Any(), int64(7)).Return(user, nil)
	m.members.EXPECT().ListOrgsForUser(gomock.Any(), int64(7)).
		Return([]models.OrgMembership{{OrgID: 3, Role: models.RoleOwner}}, nil)

	result, err := svc.Me(ctx, &auth.SessionClaims{UserID: 7, OrgID: 3, Role: models.RoleOwner})
	require.NoError(t, err)
	assert.Equal(t, user, result.User)
	assert.Equal(t, int64(3), result.ActiveOrgID)
	assert.Len(t, result.Orgs, 1)
}

func TestAuthService_Me_UserGone(t *testing.T) {
	svc, m, ctx := newAuthService(t)
	m.users.EXPECT().GetByID(gomock.Any(), int64(7)).Return(nil, nil)

	_, err := svc.Me(ctx, &auth.SessionClaims{UserID: 7})
	assert.Equal(t, apierrors.Unauthorized("account no longer exists"), err)
}

func TestAuthService_SwitchOrg(t *testing.T) {
	tests := []struct {
		desc    string
		setup   func(m authMocks)
		wantErr error
	}{
		{
			desc: "member can switch",
			setup: func(m authMocks) {
				m.members.EXPECT().GetRole(gomock.Any(), int64(5), int64(7)).Return(models.RoleMember, nil)
				m.tokens.EXPECT().IssuePair(int64(7), int64(5), models.RoleMember).Return(pairFor(7, 5), nil)
			},
		},
		{
			desc: "non-member is forbidden",
			setup: func(m authMocks) {
				m.members.EXPECT().GetRole(gomock.Any(), int64(5), int64(7)).Return("", nil)
			},
			wantErr: apierrors.Forbidden("you are not a member of this org"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newAuthService(t)
			tc.setup(m)

			pair, err := svc.SwitchOrg(ctx, 7, 5)

			if tc.wantErr != nil {
				assert.Equal(t, tc.wantErr, err)

				return
			}

			require.NoError(t, err)
			assert.NotEmpty(t, pair.AccessToken)
		})
	}
}

func TestSplitEmail(t *testing.T) {
	tests := []struct {
		in          string
		wantLocal   string
		wantDomain  string
	}{
		{in: "a@b.co", wantLocal: "a", wantDomain: "b.co"},
		{in: "no-at-sign", wantLocal: "", wantDomain: ""},
		{in: "@b.co", wantLocal: "", wantDomain: ""},
		{in: "a@", wantLocal: "", wantDomain: ""},
		{in: "a@b@c.co", wantLocal: "a@b", wantDomain: "c.co"},
	}

	for _, tc := range tests {
		local, domain := splitEmail(tc.in)
		assert.Equal(t, tc.wantLocal, local, tc.in)
		assert.Equal(t, tc.wantDomain, domain, tc.in)
	}
}
