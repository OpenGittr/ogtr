package services

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/limits"
	"github.com/opengittr/ogtr/backend/models"
)

type orgMocks struct {
	orgs    *MockOrgStore
	members *MockMemberStore
	invites *MockInviteStore
	users   *MockUserStore
}

func newOrgService(t *testing.T) (*OrgService, orgMocks, *gofr.Context) {
	t.Helper()

	return newOrgServiceWithPolicy(t, limits.Unlimited{})
}

func newOrgServiceWithPolicy(t *testing.T, policy limits.Policy) (*OrgService, orgMocks, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	m := orgMocks{
		orgs:    NewMockOrgStore(ctrl),
		members: NewMockMemberStore(ctrl),
		invites: NewMockInviteStore(ctrl),
		users:   NewMockUserStore(ctrl),
	}

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	return NewOrgService(m.orgs, m.members, m.invites, m.users, policy), m, ctx
}

// denyCreateOrgPolicy blocks org creation; embedding UnimplementedPolicy is
// the required forward-compatibility pattern for Policy implementations.
type denyCreateOrgPolicy struct {
	limits.UnimplementedPolicy

	err error
}

func (p denyCreateOrgPolicy) CanCreateOrg(*gofr.Context, int64) error { return p.err }

func expectOwner(m orgMocks, orgID, userID int64) {
	m.members.EXPECT().GetRole(gomock.Any(), orgID, userID).Return(models.RoleOwner, nil)
}

func TestOrgService_Create(t *testing.T) {
	org := &models.Org{ID: 3, Name: "Acme Rockets", Slug: "acme-rockets"}

	tests := []struct {
		desc     string
		name     string
		domain   string
		setup    func(m orgMocks)
		wantErr  bool
		wantSlug string
	}{
		{
			desc: "creates org and makes creator OWNER",
			name: "Acme Rockets",
			setup: func(m orgMocks) {
				m.orgs.EXPECT().SlugExists(gomock.Any(), "acme-rockets").Return(false, nil)
				m.orgs.EXPECT().Create(gomock.Any(), "Acme Rockets", "acme-rockets", nil).Return(org, nil)
				m.members.EXPECT().Add(gomock.Any(), int64(3), int64(7), models.RoleOwner).Return(nil)
			},
			wantSlug: "acme-rockets",
		},
		{
			desc: "slug collision gets a numeric suffix",
			name: "Acme Rockets",
			setup: func(m orgMocks) {
				m.orgs.EXPECT().SlugExists(gomock.Any(), "acme-rockets").Return(true, nil)
				m.orgs.EXPECT().SlugExists(gomock.Any(), "acme-rockets-2").Return(false, nil)
				m.orgs.EXPECT().Create(gomock.Any(), "Acme Rockets", "acme-rockets-2", nil).
					Return(&models.Org{ID: 4, Name: "Acme Rockets", Slug: "acme-rockets-2"}, nil)
				m.members.EXPECT().Add(gomock.Any(), int64(4), int64(7), models.RoleOwner).Return(nil)
			},
			wantSlug: "acme-rockets-2",
		},
		{
			desc:    "empty name rejected",
			name:    "   ",
			setup:   func(orgMocks) {},
			wantErr: true,
		},
		{
			desc:    "bad auto-join domain rejected",
			name:    "Acme",
			domain:  "not a domain",
			setup:   func(orgMocks) {},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newOrgService(t)
			tc.setup(m)

			got, err := svc.Create(ctx, 7, tc.name, tc.domain)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.wantSlug, got.Slug)
		})
	}
}

// A denying policy must block creation with 403/LIMIT_REACHED, pass the
// policy's message through verbatim, and never touch a store (the gomock
// controller fails the test on any unexpected store call).
func TestOrgService_Create_PolicyDenies(t *testing.T) {
	policy := denyCreateOrgPolicy{err: limits.Deny("org limit reached for this account")}
	svc, _, ctx := newOrgServiceWithPolicy(t, policy)

	got, err := svc.Create(ctx, 7, "Acme Rockets", "")

	assert.Nil(t, got)
	require.Error(t, err)
	assert.Equal(t, apierrors.LimitReached("org limit reached for this account"), err)

	var apiErr apierrors.Error
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusForbidden, apiErr.StatusCode())
	assert.Equal(t, map[string]any{"code": apierrors.CodeLimitReached}, apiErr.Response())
}

// A policy failure that is not a limits.Denial is an internal error, not a
// denial: it passes through untouched (Gofr will respond 500).
func TestOrgService_Create_PolicyInternalError(t *testing.T) {
	boom := errors.New("policy backend unreachable")
	svc, _, ctx := newOrgServiceWithPolicy(t, denyCreateOrgPolicy{err: boom})

	got, err := svc.Create(ctx, 7, "Acme Rockets", "")

	assert.Nil(t, got)
	require.ErrorIs(t, err, boom)

	var apiErr apierrors.Error
	assert.False(t, errors.As(err, &apiErr))
}

func TestOrgService_Get(t *testing.T) {
	svc, m, ctx := newOrgService(t)
	m.orgs.EXPECT().GetByID(gomock.Any(), int64(3)).Return(nil, nil)

	_, err := svc.Get(ctx, 3)
	assert.Equal(t, apierrors.NotFound("org not found"), err)
}

func TestOrgService_Update(t *testing.T) {
	name := "Renamed"
	empty := ""
	domain := "corp.io"
	existing := &models.Org{ID: 3, Name: "Acme", Slug: "acme"}

	tests := []struct {
		desc    string
		patch   OrgUpdate
		setup   func(m orgMocks)
		wantErr error
	}{
		{
			desc:  "owner renames and sets domain",
			patch: OrgUpdate{Name: &name, AutoJoinDomain: &domain},
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.orgs.EXPECT().GetByID(gomock.Any(), int64(3)).Return(existing, nil)
				m.orgs.EXPECT().Update(gomock.Any(), int64(3), "Renamed", &domain).Return(nil)
				m.orgs.EXPECT().GetByID(gomock.Any(), int64(3)).
					Return(&models.Org{ID: 3, Name: "Renamed", Slug: "acme", AutoJoinDomain: &domain}, nil)
			},
		},
		{
			desc:  "clearing the domain",
			patch: OrgUpdate{AutoJoinDomain: &empty},
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.orgs.EXPECT().GetByID(gomock.Any(), int64(3)).Return(existing, nil)
				m.orgs.EXPECT().Update(gomock.Any(), int64(3), "Acme", nil).Return(nil)
				m.orgs.EXPECT().GetByID(gomock.Any(), int64(3)).Return(existing, nil)
			},
		},
		{
			desc:  "non-owner forbidden",
			patch: OrgUpdate{Name: &name},
			setup: func(m orgMocks) {
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return(models.RoleMember, nil)
			},
			wantErr: apierrors.Forbidden("only an org owner can do this"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newOrgService(t)
			tc.setup(m)

			_, err := svc.Update(ctx, 3, 7, tc.patch)

			if tc.wantErr != nil {
				assert.Equal(t, tc.wantErr, err)

				return
			}

			assert.NoError(t, err)
		})
	}
}

func TestOrgService_RemoveMember(t *testing.T) {
	tests := []struct {
		desc    string
		setup   func(m orgMocks)
		wantErr error
	}{
		{
			desc: "owner removes a member",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(9)).Return(models.RoleMember, nil)
				m.members.EXPECT().Remove(gomock.Any(), int64(3), int64(9)).Return(nil)
			},
		},
		{
			desc: "last owner cannot be removed (including self)",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return(models.RoleOwner, nil)
				m.members.EXPECT().CountOwners(gomock.Any(), int64(3)).Return(1, nil)
			},
			wantErr: apierrors.Unprocessable("cannot remove the last owner of the org"),
		},
		{
			desc: "an owner can be removed when another owner remains",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(9)).Return(models.RoleOwner, nil)
				m.members.EXPECT().CountOwners(gomock.Any(), int64(3)).Return(2, nil)
				m.members.EXPECT().Remove(gomock.Any(), int64(3), int64(9)).Return(nil)
			},
		},
		{
			desc: "target not a member",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(9)).Return("", nil)
			},
			wantErr: apierrors.NotFound("user is not a member of this org"),
		},
		{
			desc: "member cannot remove anyone",
			setup: func(m orgMocks) {
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return(models.RoleMember, nil)
			},
			wantErr: apierrors.Forbidden("only an org owner can do this"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newOrgService(t)
			tc.setup(m)

			targetID := int64(9)
			if tc.desc == "last owner cannot be removed (including self)" {
				targetID = 7
			}

			err := svc.RemoveMember(ctx, 3, 7, targetID)

			if tc.wantErr != nil {
				assert.Equal(t, tc.wantErr, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestOrgService_CreateInvite(t *testing.T) {
	invite := &models.Invite{ID: 12, OrgID: 3, Email: "new@other.io", Status: models.InviteStatusPending}

	tests := []struct {
		desc    string
		email   string
		setup   func(m orgMocks)
		wantErr error
	}{
		{
			desc:  "owner invites a new email",
			email: "New@Other.io",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.users.EXPECT().GetByEmail(gomock.Any(), "new@other.io").Return(nil, nil)
				m.invites.EXPECT().HasPending(gomock.Any(), int64(3), "new@other.io").Return(false, nil)
				m.invites.EXPECT().Create(gomock.Any(), int64(3), "new@other.io", int64(7)).Return(invite, nil)
			},
		},
		{
			desc:  "already a member",
			email: "member@x.co",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.users.EXPECT().GetByEmail(gomock.Any(), "member@x.co").
					Return(&models.User{ID: 9, Email: "member@x.co"}, nil)
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(9)).Return(models.RoleMember, nil)
			},
			wantErr: apierrors.Conflict("user is already a member of this org"),
		},
		{
			desc:  "duplicate pending invite",
			email: "new@other.io",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.users.EXPECT().GetByEmail(gomock.Any(), "new@other.io").Return(nil, nil)
				m.invites.EXPECT().HasPending(gomock.Any(), int64(3), "new@other.io").Return(true, nil)
			},
			wantErr: apierrors.Conflict("a pending invite for this email already exists"),
		},
		{
			desc:  "invalid email",
			email: "nonsense",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
			},
			wantErr: apierrors.Unprocessable("invalid email address"),
		},
		{
			desc:  "member cannot invite",
			email: "new@other.io",
			setup: func(m orgMocks) {
				m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return(models.RoleMember, nil)
			},
			wantErr: apierrors.Forbidden("only an org owner can do this"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newOrgService(t)
			tc.setup(m)

			got, err := svc.CreateInvite(ctx, 3, 7, tc.email)

			if tc.wantErr != nil {
				assert.Equal(t, tc.wantErr, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, invite, got)
		})
	}
}

func TestOrgService_RevokeInvite(t *testing.T) {
	tests := []struct {
		desc    string
		setup   func(m orgMocks)
		wantErr error
	}{
		{
			desc: "owner revokes a pending invite",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.invites.EXPECT().GetByID(gomock.Any(), int64(3), int64(12)).
					Return(&models.Invite{ID: 12, OrgID: 3, Status: models.InviteStatusPending}, nil)
				m.invites.EXPECT().SetStatus(gomock.Any(), int64(12), models.InviteStatusRevoked).Return(nil)
			},
		},
		{
			desc: "invite from another org is not found",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.invites.EXPECT().GetByID(gomock.Any(), int64(3), int64(12)).Return(nil, nil)
			},
			wantErr: apierrors.NotFound("invite not found"),
		},
		{
			desc: "already accepted invite cannot be revoked",
			setup: func(m orgMocks) {
				expectOwner(m, 3, 7)
				m.invites.EXPECT().GetByID(gomock.Any(), int64(3), int64(12)).
					Return(&models.Invite{ID: 12, OrgID: 3, Status: models.InviteStatusAccepted}, nil)
			},
			wantErr: apierrors.Conflict("invite is not pending"),
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newOrgService(t)
			tc.setup(m)

			err := svc.RevokeInvite(ctx, 3, 7, 12)

			if tc.wantErr != nil {
				assert.Equal(t, tc.wantErr, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "Acme Rockets", want: "acme-rockets"},
		{in: "  Hello,   World! ", want: "hello-world"},
		{in: "ALLCAPS", want: "allcaps"},
		{in: "___", want: "org"},
		{in: "a--b__c", want: "a-b-c"},
		{in: "टीम", want: "org"},
	}

	for _, tc := range tests {
		assert.Equal(t, tc.want, slugify(tc.in), tc.in)
	}
}
