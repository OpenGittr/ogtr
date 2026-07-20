package services

// Per-axis regression tests for the limits.Policy seam: every axis's denial
// maps to 403 LIMIT_REACHED with the policy's message verbatim and stops the
// action before any store write (the gomock controllers fail the tests on any
// unexpected store call). The login-path membership axis additionally proves
// a denial can never fail a login.

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/auth"
	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

// axisPolicy denies (or errors) one chosen axis and records the ids the check
// received — embedding limits.UnimplementedPolicy, the required pattern, so
// every other axis stays allowed.
type axisPolicy struct {
	limits.UnimplementedPolicy

	axis string
	err  error

	gotOrgID  int64
	gotUserID int64
}

func (p *axisPolicy) errFor(axis string) error {
	if p.axis == axis {
		return p.err
	}

	return nil
}

func (p *axisPolicy) CanCreateLink(_ *gofr.Context, orgID, userID int64) error {
	p.gotOrgID, p.gotUserID = orgID, userID

	return p.errFor("link")
}

func (p *axisPolicy) CanAddDomain(_ *gofr.Context, orgID int64) error {
	p.gotOrgID = orgID

	return p.errFor("domain")
}

func (p *axisPolicy) CanAddMember(_ *gofr.Context, orgID int64) error {
	p.gotOrgID = orgID

	return p.errFor("member")
}

func (p *axisPolicy) CanCreateAPIKey(_ *gofr.Context, orgID int64) error {
	p.gotOrgID = orgID

	return p.errFor("apikey")
}

// assertLimitReached asserts the full denial contract: 403, machine-readable
// code LIMIT_REACHED, and the policy's message passed through verbatim.
func assertLimitReached(t *testing.T, err error, wantMsg string) {
	t.Helper()

	require.Error(t, err)
	assert.Equal(t, apierrors.LimitReached(wantMsg), err)

	var apiErr apierrors.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode())
	assert.Equal(t, map[string]any{"code": apierrors.CodeLimitReached}, apiErr.Response())
}

func TestLinkService_Shorten_PolicyDenies(t *testing.T) {
	tests := []struct {
		desc       string
		viaAPIKey  bool
		wantUserID int64
	}{
		{desc: "JWT path: denial carries the creating user", wantUserID: 7},
		{desc: "API-key path: denial sees userID 0", viaAPIKey: true, wantUserID: 0},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			policy := &axisPolicy{axis: "link", err: limits.Deny("no more links this month")}
			svc, _, _, _, ctx := newLinkServiceWithPolicy(t, policy)

			var (
				got *models.Link
				err error
			)

			if tc.viaAPIKey {
				got, err = svc.ShortenViaAPIKey(ctx, 3, 55, ShortenInput{URL: "https://example.com"})
			} else {
				got, err = svc.Shorten(ctx, 3, 7, ShortenInput{URL: "https://example.com"})
			}

			assert.Nil(t, got)
			assertLimitReached(t, err, "no more links this month")
			assert.Equal(t, int64(3), policy.gotOrgID)
			assert.Equal(t, tc.wantUserID, policy.gotUserID)
		})
	}
}

// Edits are never policy-checked (the axis covers creation only): a policy
// that denies link creation must not block a destination edit.
func TestLinkService_UpdateDestination_NotPolicyChecked(t *testing.T) {
	policy := &axisPolicy{axis: "link", err: limits.Deny("no more links this month")}
	svc, links, _, domains, ctx := newLinkServiceWithPolicy(t, policy)

	domains.EXPECT().PrimaryVerifiedHostname(gomock.Any(), gomock.Any()).Return("", nil).AnyTimes()

	link := publicLink(9, "abc1234", "https://old.example.com")
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(link, nil).Times(2)
	links.EXPECT().UpdateDestination(gomock.Any(), int64(3), int64(9), "https://new.example.com",
		nil, nil, nil).Return(nil)
	links.EXPECT().InsertEdit(gomock.Any(), gomock.Any()).Return(nil)

	_, err := svc.UpdateDestination(ctx, 3, 7, 9, EditInput{URL: "https://new.example.com"})

	require.NoError(t, err, "a link-creation denial must not block editing an existing link")
}

func TestDomainService_Create_PolicyDenies(t *testing.T) {
	policy := &axisPolicy{axis: "domain", err: limits.Deny("no more custom domains")}
	svc, m, ctx := newDomainServiceWithPolicy(t, policy)

	stubOwner(m)

	got, err := svc.Create(ctx, 3, 7, "links.example.com")

	assert.Nil(t, got)
	assertLimitReached(t, err, "no more custom domains")
	assert.Equal(t, int64(3), policy.gotOrgID)
}

func TestOrgService_CreateInvite_PolicyDenies(t *testing.T) {
	policy := &axisPolicy{axis: "member", err: limits.Deny("no more seats in this org")}
	svc, m, ctx := newOrgServiceWithPolicy(t, policy)

	expectOwner(m, 3, 7)

	got, err := svc.CreateInvite(ctx, 3, 7, "new@acme.com")

	assert.Nil(t, got)
	assertLimitReached(t, err, "no more seats in this org")
	assert.Equal(t, int64(3), policy.gotOrgID)
}

func TestAPIKeyService_Create_PolicyDenies(t *testing.T) {
	policy := &axisPolicy{axis: "apikey", err: limits.Deny("no more API keys")}
	svc, _, ctx := newAPIKeyServiceWithPolicy(t, policy)

	got, err := svc.Create(ctx, 3, "ci key")

	assert.Nil(t, got)
	assertLimitReached(t, err, "no more API keys")
	assert.Equal(t, int64(3), policy.gotOrgID)
}

// A non-Denial policy error on any axis is an internal failure, never a
// LIMIT_REACHED denial.
func TestPolicyInternalError_IsNotADenial(t *testing.T) {
	boom := errors.New("policy backend unreachable")

	tests := []struct {
		desc string
		axis string
		call func(t *testing.T, policy limits.Policy) error
	}{
		{desc: "link", axis: "link", call: func(t *testing.T, policy limits.Policy) error {
			t.Helper()
			svc, _, _, _, ctx := newLinkServiceWithPolicy(t, policy)
			_, err := svc.Shorten(ctx, 3, 7, ShortenInput{URL: "https://example.com"})

			return err
		}},
		{desc: "domain", axis: "domain", call: func(t *testing.T, policy limits.Policy) error {
			t.Helper()
			svc, m, ctx := newDomainServiceWithPolicy(t, policy)
			stubOwner(m)
			_, err := svc.Create(ctx, 3, 7, "links.example.com")

			return err
		}},
		{desc: "member", axis: "member", call: func(t *testing.T, policy limits.Policy) error {
			t.Helper()
			svc, m, ctx := newOrgServiceWithPolicy(t, policy)
			expectOwner(m, 3, 7)
			_, err := svc.CreateInvite(ctx, 3, 7, "new@acme.com")

			return err
		}},
		{desc: "apikey", axis: "apikey", call: func(t *testing.T, policy limits.Policy) error {
			t.Helper()
			svc, _, ctx := newAPIKeyServiceWithPolicy(t, policy)
			_, err := svc.Create(ctx, 3, "ci key")

			return err
		}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			err := tc.call(t, &axisPolicy{axis: tc.axis, err: boom})

			require.ErrorIs(t, err, boom)

			var apiErr apierrors.Error
			assert.False(t, errors.As(err, &apiErr), "internal policy failures carry no API status")
		})
	}
}

// ---------------------------------------------------------------------------
// Login-path membership choke point: a CanAddMember denial skips the join but
// NEVER fails the login.
// ---------------------------------------------------------------------------

func TestAuthService_Login_AutoJoinDeniedByPolicy(t *testing.T) {
	policy := &axisPolicy{axis: "member", err: limits.Deny("no more seats in this org")}
	svc, m, ctx := newAuthServiceWithPolicy(t, policy)

	user := &models.User{ID: 7, Name: "Alice", Email: "alice@acme.com", Status: models.UserStatusEnabled}
	org := &models.Org{ID: 3, Name: "Acme", Slug: "acme"}

	m.provider.EXPECT().Verify(gomock.Any(), "cred").
		Return(auth.Identity{Email: "alice@acme.com", Name: "Alice"}, nil)
	m.users.EXPECT().GetByEmail(gomock.Any(), "alice@acme.com").Return(user, nil)
	m.invites.EXPECT().PendingForEmail(gomock.Any(), "alice@acme.com").Return([]models.Invite{}, nil)
	m.members.EXPECT().ListOrgsForUser(gomock.Any(), int64(7)).Return([]models.OrgMembership{}, nil)
	m.orgs.EXPECT().GetByAutoJoinDomain(gomock.Any(), "acme.com").Return(org, nil)
	// NO members.Add — the denial skips the membership write entirely.
	m.tokens.EXPECT().IssuePair(int64(7), int64(0), "").Return(pairFor(7, 0), nil)

	result, err := svc.Login(ctx, auth.ProviderGoogle, "cred")

	require.NoError(t, err, "a membership denial must never fail a login")
	assert.Empty(t, result.Orgs, "the user simply stays org-less")
	assert.Zero(t, result.ActiveOrgID)
}

func TestAuthService_Login_InviteAcceptanceDeniedByPolicy(t *testing.T) {
	policy := &axisPolicy{axis: "member", err: limits.Deny("no more seats in this org")}
	svc, m, ctx := newAuthServiceWithPolicy(t, policy)

	user := &models.User{ID: 7, Name: "Alice", Email: "alice@acme.com", Status: models.UserStatusEnabled}
	invite := models.Invite{ID: 12, OrgID: 3, Email: "alice@acme.com", Status: models.InviteStatusPending}

	m.provider.EXPECT().Verify(gomock.Any(), "cred").
		Return(auth.Identity{Email: "alice@acme.com", Name: "Alice"}, nil)
	m.users.EXPECT().GetByEmail(gomock.Any(), "alice@acme.com").Return(user, nil)
	m.invites.EXPECT().PendingForEmail(gomock.Any(), "alice@acme.com").Return([]models.Invite{invite}, nil)
	m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return("", nil)
	// NO members.Add and NO invites.SetStatus — the invite stays PENDING so it
	// converts on a later login once the policy allows.
	m.orgs.EXPECT().GetByAutoJoinDomain(gomock.Any(), "acme.com").Return(nil, nil)
	m.members.EXPECT().ListOrgsForUser(gomock.Any(), int64(7)).Return([]models.OrgMembership{}, nil)
	m.tokens.EXPECT().IssuePair(int64(7), int64(0), "").Return(pairFor(7, 0), nil)

	result, err := svc.Login(ctx, auth.ProviderGoogle, "cred")

	require.NoError(t, err, "an invite-conversion denial must never fail a login")
	assert.Empty(t, result.Orgs)
}

// An internal (non-Denial) policy failure on the login path does propagate:
// it is a server fault, not a policy decision.
func TestAuthService_Login_PolicyInternalErrorPropagates(t *testing.T) {
	boom := errors.New("policy backend unreachable")
	policy := &axisPolicy{axis: "member", err: boom}
	svc, m, ctx := newAuthServiceWithPolicy(t, policy)

	user := &models.User{ID: 7, Name: "Alice", Email: "alice@acme.com", Status: models.UserStatusEnabled}
	org := &models.Org{ID: 3, Name: "Acme", Slug: "acme"}

	m.provider.EXPECT().Verify(gomock.Any(), "cred").
		Return(auth.Identity{Email: "alice@acme.com", Name: "Alice"}, nil)
	m.users.EXPECT().GetByEmail(gomock.Any(), "alice@acme.com").Return(user, nil)
	m.invites.EXPECT().PendingForEmail(gomock.Any(), "alice@acme.com").Return([]models.Invite{}, nil)
	m.members.EXPECT().ListOrgsForUser(gomock.Any(), int64(7)).Return([]models.OrgMembership{}, nil)
	m.orgs.EXPECT().GetByAutoJoinDomain(gomock.Any(), "acme.com").Return(org, nil)

	_, err := svc.Login(ctx, auth.ProviderGoogle, "cred")

	require.ErrorIs(t, err, boom)
}
