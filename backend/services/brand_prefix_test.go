package services

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/models"
)

type prefixMocks struct {
	prefixes *MockBrandPrefixStore
	links    *MockLinkStore
	members  *MockMemberStore
}

func newBrandPrefixService(t *testing.T, autoApprove bool) (*BrandPrefixService, prefixMocks, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	m := prefixMocks{
		prefixes: NewMockBrandPrefixStore(ctrl),
		links:    NewMockLinkStore(ctrl),
		members:  NewMockMemberStore(ctrl),
	}

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	return NewBrandPrefixService(m.prefixes, m.links, m.members, nil, autoApprove), m, ctx
}

func stubPrefixOwner(m prefixMocks) {
	m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return(models.RoleOwner, nil)
}

// pngBytes is the PNG signature plus padding — enough for content sniffing.
var pngBytes = append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)

func validPrefixInput() BrandPrefixInput {
	return BrandPrefixInput{
		Prefix: "ACMEIN", EntityID: "1101234567890123456", Header: "acmein", EntityName: "Acme India Pvt Ltd",
		Screenshot: pngBytes,
	}
}

func TestBrandPrefixService_CreateValidation(t *testing.T) {
	tests := []struct {
		desc string
		edit func(in *BrandPrefixInput)
	}{
		{"too short", func(in *BrandPrefixInput) { in.Prefix, in.Header = "AB", "AB" }},
		{"too long", func(in *BrandPrefixInput) { in.Prefix = "ABCDEFGHIJKLMNOPQRSTU"; in.Header = in.Prefix }},
		{"non-alphanumeric", func(in *BrandPrefixInput) { in.Prefix, in.Header = "ACME-IN", "ACME-IN" }},
		{"unknown registry", func(in *BrandPrefixInput) { in.Registry = "US-10DLC" }},
		{"non-numeric entity id", func(in *BrandPrefixInput) { in.EntityID = "PE-123" }},
		{"header differs from prefix", func(in *BrandPrefixInput) { in.Header = "ACMEXX" }},
		{"entity name too short", func(in *BrandPrefixInput) { in.EntityName = "A" }},
		{"reserved word", func(in *BrandPrefixInput) { in.Prefix, in.Header = "login", "login" }},
		{"functional word", func(in *BrandPrefixInput) { in.Prefix, in.Header = "api", "api" }},
		{"no screenshot", func(in *BrandPrefixInput) { in.Screenshot = nil }},
		{"screenshot not an image", func(in *BrandPrefixInput) { in.Screenshot = []byte("<html><script>") }},
		{"screenshot too large", func(in *BrandPrefixInput) {
			in.Screenshot = append(append([]byte{}, pngBytes...), make([]byte, MaxScreenshotBytes)...)
		}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newBrandPrefixService(t, false)
			stubPrefixOwner(m)

			in := validPrefixInput()
			tc.edit(&in)

			_, err := svc.Create(ctx, 3, 7, in)

			assertStatus(t, err, http.StatusUnprocessableEntity)
		})
	}
}

func TestBrandPrefixService_CreatePending(t *testing.T) {
	svc, m, ctx := newBrandPrefixService(t, false)
	stubPrefixOwner(m)
	m.prefixes.EXPECT().CountHeldByOrg(gomock.Any(), int64(3)).Return(0, nil)
	m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "ACMEIN").Return(nil, nil)
	m.links.EXPECT().CodeExists(gomock.Any(), "ACMEIN").Return(false, nil)
	m.prefixes.EXPECT().Create(gomock.Any(), &models.BrandPrefix{
		OrgID: 3, Prefix: "ACMEIN", Status: models.BrandPrefixStatusPending, Registry: "IN-DLT",
		EntityID: "1101234567890123456", Header: "acmein", EntityName: "Acme India Pvt Ltd",
	}, &models.BrandPrefixScreenshot{ContentType: "image/png", Image: pngBytes}).Return(brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusPending), nil)

	got, err := svc.Create(ctx, 3, 7, validPrefixInput())

	require.NoError(t, err)
	assert.Equal(t, models.BrandPrefixStatusPending, got.Status)
}

func TestBrandPrefixService_CreateAutoApproved(t *testing.T) {
	svc, m, ctx := newBrandPrefixService(t, true)
	stubPrefixOwner(m)
	m.prefixes.EXPECT().CountHeldByOrg(gomock.Any(), int64(3)).Return(0, nil)
	m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "ACMEIN").Return(nil, nil)
	m.links.EXPECT().CodeExists(gomock.Any(), "ACMEIN").Return(false, nil)
	m.prefixes.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, p *models.BrandPrefix, _ *models.BrandPrefixScreenshot) (*models.BrandPrefix, error) {
			assert.Equal(t, models.BrandPrefixStatusApproved, p.Status)

			return brandPrefix(5, 3, p.Prefix, p.Status), nil
		})

	_, err := svc.Create(ctx, 3, 7, validPrefixInput())

	require.NoError(t, err)
}

func TestBrandPrefixService_CreateConflicts(t *testing.T) {
	tests := []struct {
		desc  string
		setup func(m prefixMocks)
		want  int
	}{
		{
			desc: "held by another org",
			setup: func(m prefixMocks) {
				m.prefixes.EXPECT().CountHeldByOrg(gomock.Any(), int64(3)).Return(0, nil)
				m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "ACMEIN").
					Return(brandPrefix(9, 99, "acmein", models.BrandPrefixStatusRevoked), nil)
			},
			want: http.StatusConflict,
		},
		{
			desc: "equals an existing short code",
			setup: func(m prefixMocks) {
				m.prefixes.EXPECT().CountHeldByOrg(gomock.Any(), int64(3)).Return(0, nil)
				m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "ACMEIN").Return(nil, nil)
				m.links.EXPECT().CodeExists(gomock.Any(), "ACMEIN").Return(true, nil)
			},
			want: http.StatusConflict,
		},
		{
			desc: "org at the cap",
			setup: func(m prefixMocks) {
				m.prefixes.EXPECT().CountHeldByOrg(gomock.Any(), int64(3)).Return(maxPrefixesPerOrg, nil)
			},
			want: http.StatusUnprocessableEntity,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, m, ctx := newBrandPrefixService(t, false)
			stubPrefixOwner(m)
			tc.setup(m)

			_, err := svc.Create(ctx, 3, 7, validPrefixInput())

			assertStatus(t, err, tc.want)
		})
	}
}

func TestBrandPrefixService_RejectedValueIsClaimable(t *testing.T) {
	svc, m, ctx := newBrandPrefixService(t, false)
	stubPrefixOwner(m)
	m.prefixes.EXPECT().CountHeldByOrg(gomock.Any(), int64(3)).Return(0, nil)
	m.prefixes.EXPECT().GetByPrefix(gomock.Any(), "ACMEIN").
		Return(brandPrefix(9, 99, "ACMEIN", models.BrandPrefixStatusRejected), nil)
	m.links.EXPECT().CodeExists(gomock.Any(), "ACMEIN").Return(false, nil)
	m.prefixes.EXPECT().Create(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(brandPrefix(10, 3, "ACMEIN", models.BrandPrefixStatusPending), nil)

	_, err := svc.Create(ctx, 3, 7, validPrefixInput())

	require.NoError(t, err)
}

func TestBrandPrefixService_MembersCannotMutate(t *testing.T) {
	svc, m, ctx := newBrandPrefixService(t, false)
	m.members.EXPECT().GetRole(gomock.Any(), int64(3), int64(7)).Return(models.RoleMember, nil).Times(3)

	_, err := svc.Create(ctx, 3, 7, validPrefixInput())
	assertStatus(t, err, http.StatusForbidden)

	_, err = svc.SetPrimary(ctx, 3, 7, 5)
	assertStatus(t, err, http.StatusForbidden)

	err = svc.Delete(ctx, 3, 7, 5)
	assertStatus(t, err, http.StatusForbidden)
}

func TestBrandPrefixService_SetPrimary(t *testing.T) {
	t.Run("approved becomes primary", func(t *testing.T) {
		svc, m, ctx := newBrandPrefixService(t, false)
		stubPrefixOwner(m)
		approved := brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusApproved)
		m.prefixes.EXPECT().GetByID(gomock.Any(), int64(3), int64(5)).Return(approved, nil).Times(2)
		m.prefixes.EXPECT().SetPrimary(gomock.Any(), int64(3), int64(5)).Return(true, nil)

		_, err := svc.SetPrimary(ctx, 3, 7, 5)

		require.NoError(t, err)
	})

	t.Run("pending is 422", func(t *testing.T) {
		svc, m, ctx := newBrandPrefixService(t, false)
		stubPrefixOwner(m)
		m.prefixes.EXPECT().GetByID(gomock.Any(), int64(3), int64(5)).
			Return(brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusPending), nil)

		_, err := svc.SetPrimary(ctx, 3, 7, 5)

		assertStatus(t, err, http.StatusUnprocessableEntity)
	})

	t.Run("unknown is 404", func(t *testing.T) {
		svc, m, ctx := newBrandPrefixService(t, false)
		stubPrefixOwner(m)
		m.prefixes.EXPECT().GetByID(gomock.Any(), int64(3), int64(5)).Return(nil, nil)

		_, err := svc.SetPrimary(ctx, 3, 7, 5)

		assertStatus(t, err, http.StatusNotFound)
	})
}

func TestBrandPrefixService_Delete(t *testing.T) {
	t.Run("pending withdrawn", func(t *testing.T) {
		svc, m, ctx := newBrandPrefixService(t, false)
		stubPrefixOwner(m)
		m.prefixes.EXPECT().GetByID(gomock.Any(), int64(3), int64(5)).
			Return(brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusPending), nil)
		m.prefixes.EXPECT().Delete(gomock.Any(), int64(3), int64(5)).Return(true, nil)

		require.NoError(t, svc.Delete(ctx, 3, 7, 5))
	})

	t.Run("revoked is held", func(t *testing.T) {
		svc, m, ctx := newBrandPrefixService(t, false)
		stubPrefixOwner(m)
		m.prefixes.EXPECT().GetByID(gomock.Any(), int64(3), int64(5)).
			Return(brandPrefix(5, 3, "ACMEIN", models.BrandPrefixStatusRevoked), nil)

		assertStatus(t, svc.Delete(ctx, 3, 7, 5), http.StatusConflict)
	})
}
