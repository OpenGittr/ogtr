package services

import (
	"net/http"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/models"
)

func adminPrefix(status string) *models.AdminBrandPrefix {
	return &models.AdminBrandPrefix{
		BrandPrefix: models.BrandPrefix{ID: 5, OrgID: 3, Prefix: "ACMEIN", Status: status},
		OrgName:     "Acme",
	}
}

func TestAdminService_Prefixes(t *testing.T) {
	t.Run("defaults to the pending queue", func(t *testing.T) {
		svc, store, _, ctx := newAdminService(t)
		store.EXPECT().ListPrefixes(gomock.Any(), models.BrandPrefixStatusPending, adminPageSize, 0).
			Return([]models.AdminBrandPrefix{*adminPrefix(models.BrandPrefixStatusPending)}, nil)
		store.EXPECT().CountPrefixes(gomock.Any(), models.BrandPrefixStatusPending).Return(int64(1), nil)

		page, err := svc.Prefixes(ctx, "", 1)

		require.NoError(t, err)
		assert.Equal(t, int64(1), page.Total)
		assert.Len(t, page.Prefixes, 1)
	})

	t.Run("status is case-insensitive", func(t *testing.T) {
		svc, store, _, ctx := newAdminService(t)
		store.EXPECT().ListPrefixes(gomock.Any(), models.BrandPrefixStatusRevoked, adminPageSize, adminPageSize).
			Return([]models.AdminBrandPrefix{}, nil)
		store.EXPECT().CountPrefixes(gomock.Any(), models.BrandPrefixStatusRevoked).Return(int64(0), nil)

		_, err := svc.Prefixes(ctx, "revoked", 2)

		require.NoError(t, err)
	})

	t.Run("unknown status is 422", func(t *testing.T) {
		svc, _, _, ctx := newAdminService(t)

		_, err := svc.Prefixes(ctx, "DONE", 1)

		assertStatus(t, err, http.StatusUnprocessableEntity)
	})
}

func TestAdminService_PrefixDecisions(t *testing.T) {
	t.Run("approve pending", func(t *testing.T) {
		svc, store, _, ctx := newAdminService(t)
		store.EXPECT().GetPrefix(gomock.Any(), int64(5)).Return(adminPrefix(models.BrandPrefixStatusPending), nil)
		store.EXPECT().DecidePrefix(gomock.Any(), int64(5), models.BrandPrefixStatusPending,
			models.BrandPrefixStatusApproved, (*string)(nil)).Return(true, nil)
		store.EXPECT().GetPrefix(gomock.Any(), int64(5)).Return(adminPrefix(models.BrandPrefixStatusApproved), nil)

		got, err := svc.ApprovePrefix(ctx, 5)

		require.NoError(t, err)
		assert.Equal(t, models.BrandPrefixStatusApproved, got.Status)
	})

	t.Run("reject records the reason", func(t *testing.T) {
		svc, store, _, ctx := newAdminService(t)
		reason := "header not registered to this entity"
		store.EXPECT().GetPrefix(gomock.Any(), int64(5)).Return(adminPrefix(models.BrandPrefixStatusPending), nil)
		store.EXPECT().DecidePrefix(gomock.Any(), int64(5), models.BrandPrefixStatusPending,
			models.BrandPrefixStatusRejected, &reason).Return(true, nil)
		store.EXPECT().GetPrefix(gomock.Any(), int64(5)).Return(adminPrefix(models.BrandPrefixStatusRejected), nil)

		_, err := svc.RejectPrefix(ctx, 5, "  "+reason+" ")

		require.NoError(t, err)
	})

	t.Run("reject and revoke need a reason", func(t *testing.T) {
		svc, _, _, ctx := newAdminService(t)

		_, err := svc.RejectPrefix(ctx, 5, " ")
		assertStatus(t, err, http.StatusUnprocessableEntity)

		_, err = svc.RevokePrefix(ctx, 5, "")
		assertStatus(t, err, http.StatusUnprocessableEntity)

		_, err = svc.RevokePrefix(ctx, 5, strings.Repeat("x", 501))
		assertStatus(t, err, http.StatusUnprocessableEntity)
	})

	t.Run("stale decision is 409", func(t *testing.T) {
		svc, store, _, ctx := newAdminService(t)
		store.EXPECT().GetPrefix(gomock.Any(), int64(5)).Return(adminPrefix(models.BrandPrefixStatusApproved), nil)
		store.EXPECT().DecidePrefix(gomock.Any(), int64(5), models.BrandPrefixStatusPending,
			models.BrandPrefixStatusApproved, (*string)(nil)).Return(false, nil)

		_, err := svc.ApprovePrefix(ctx, 5)

		assertStatus(t, err, http.StatusConflict)
	})

	t.Run("unknown prefix is 404", func(t *testing.T) {
		svc, store, _, ctx := newAdminService(t)
		store.EXPECT().GetPrefix(gomock.Any(), int64(5)).Return(nil, nil)

		_, err := svc.RevokePrefix(ctx, 5, "misuse reported")

		assertStatus(t, err, http.StatusNotFound)
	})
}

func TestAdminService_ReleasePrefix(t *testing.T) {
	t.Run("revoked is released", func(t *testing.T) {
		svc, store, _, ctx := newAdminService(t)
		store.EXPECT().GetPrefix(gomock.Any(), int64(5)).Return(adminPrefix(models.BrandPrefixStatusRevoked), nil)
		store.EXPECT().ReleasePrefix(gomock.Any(), int64(5)).Return(true, nil)

		require.NoError(t, svc.ReleasePrefix(ctx, 5))
	})

	t.Run("non-revoked is 409", func(t *testing.T) {
		svc, store, _, ctx := newAdminService(t)
		store.EXPECT().GetPrefix(gomock.Any(), int64(5)).Return(adminPrefix(models.BrandPrefixStatusApproved), nil)
		store.EXPECT().ReleasePrefix(gomock.Any(), int64(5)).Return(false, nil)

		assertStatus(t, svc.ReleasePrefix(ctx, 5), http.StatusConflict)
	})
}
