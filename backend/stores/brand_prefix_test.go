package stores

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opengittr/ogtr/backend/models"
)

const (
	selectPrefixPrefix = "SELECT " + brandPrefixColumns + " FROM brand_prefixes "
	selectPrefixByID   = selectPrefixPrefix + "WHERE id = ? AND org_id = ?"
	selectPrefixByVal  = selectPrefixPrefix + "WHERE prefix = ?"
	isHeldQ            = "SELECT 1 FROM brand_prefixes WHERE prefix = ? AND status <> ? LIMIT 1"
	primaryPrefixQ     = "SELECT prefix FROM brand_prefixes WHERE org_id = ? AND is_primary = 1 AND status = ? LIMIT 1"
	clearPrefixPrimary = "UPDATE brand_prefixes SET is_primary = 0 WHERE org_id = ?"
	setPrefixPrimary   = "UPDATE brand_prefixes SET is_primary = 1 WHERE id = ? AND org_id = ? AND status = ?"
	deletePrefixQ      = "DELETE FROM brand_prefixes WHERE id = ? AND org_id = ? AND status <> ?"
)

func prefixRows(id int64, prefix, status string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "org_id", "prefix", "status", "is_primary", "registry", "entity_id",
		"header", "entity_name", "decision_reason", "decided_at", "created_at"}).
		AddRow(id, 3, prefix, status, false, "IN-DLT", "1101234567890123456", prefix, "Acme", nil, nil, time.Now())
}

func TestBrandPrefixStore_Create(t *testing.T) {
	in := &models.BrandPrefix{
		OrgID: 3, Prefix: "ACMEIN", Status: "PENDING", Registry: "IN-DLT",
		EntityID: "1101234567890123456", Header: "ACMEIN", EntityName: "Acme",
	}

	t.Run("rejected row cleared and request inserted in one transaction", func(t *testing.T) {
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectBegin()
		mocks.SQL.ExpectExec(deleteRejectedPrefix).WithArgs("ACMEIN", "REJECTED").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mocks.SQL.ExpectExec(insertBrandPrefix).
			WithArgs(int64(3), "ACMEIN", "PENDING", "IN-DLT", "1101234567890123456", "ACMEIN", "Acme", "PENDING").
			WillReturnResult(sqlmock.NewResult(5, 1))
		mocks.SQL.ExpectCommit()
		mocks.SQL.ExpectQuery(selectPrefixByID).WithArgs(int64(5), int64(3)).
			WillReturnRows(prefixRows(5, "ACMEIN", "PENDING"))

		got, err := NewBrandPrefixStore().Create(ctx, in)

		require.NoError(t, err)
		assert.Equal(t, int64(5), got.ID)
	})

	t.Run("insert error rolls back", func(t *testing.T) {
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectBegin()
		mocks.SQL.ExpectExec(deleteRejectedPrefix).WithArgs("ACMEIN", "REJECTED").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mocks.SQL.ExpectExec(insertBrandPrefix).WillReturnError(errDB)
		mocks.SQL.ExpectRollback()

		_, err := NewBrandPrefixStore().Create(ctx, in)

		require.Error(t, err)
	})
}

func TestBrandPrefixStore_GetByPrefix(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectPrefixByVal).WithArgs("acmein").
		WillReturnRows(prefixRows(5, "ACMEIN", "APPROVED"))
	mocks.SQL.ExpectQuery(selectPrefixByVal).WithArgs("nope").
		WillReturnRows(sqlmock.NewRows(nil))

	got, err := NewBrandPrefixStore().GetByPrefix(ctx, "acmein")
	require.NoError(t, err)
	assert.Equal(t, "ACMEIN", got.Prefix, "stored as entered, matched case-insensitively")

	missing, err := NewBrandPrefixStore().GetByPrefix(ctx, "nope")
	require.NoError(t, err)
	assert.Nil(t, missing)
}

func TestBrandPrefixStore_IsHeld(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(isHeldQ).WithArgs("ACMEIN", "REJECTED").
		WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
	mocks.SQL.ExpectQuery(isHeldQ).WithArgs("free", "REJECTED").
		WillReturnRows(sqlmock.NewRows([]string{"1"}))

	held, err := NewBrandPrefixStore().IsHeld(ctx, "ACMEIN")
	require.NoError(t, err)
	assert.True(t, held)

	held, err = NewBrandPrefixStore().IsHeld(ctx, "free")
	require.NoError(t, err)
	assert.False(t, held)
}

func TestBrandPrefixStore_PrimaryApprovedPrefix(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(primaryPrefixQ).WithArgs(int64(3), "APPROVED").
		WillReturnRows(sqlmock.NewRows([]string{"prefix"}).AddRow("ACMEIN"))
	mocks.SQL.ExpectQuery(primaryPrefixQ).WithArgs(int64(4), "APPROVED").
		WillReturnRows(sqlmock.NewRows([]string{"prefix"}))

	got, err := NewBrandPrefixStore().PrimaryApprovedPrefix(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, "ACMEIN", got)

	got, err = NewBrandPrefixStore().PrimaryApprovedPrefix(ctx, 4)
	require.NoError(t, err)
	assert.Empty(t, got)
}

func TestBrandPrefixStore_SetPrimary(t *testing.T) {
	t.Run("approved swap commits", func(t *testing.T) {
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectBegin()
		mocks.SQL.ExpectExec(clearPrefixPrimary).WithArgs(int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
		mocks.SQL.ExpectExec(setPrefixPrimary).WithArgs(int64(5), int64(3), "APPROVED").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mocks.SQL.ExpectCommit()

		swapped, err := NewBrandPrefixStore().SetPrimary(ctx, 3, 5)
		require.NoError(t, err)
		assert.True(t, swapped)
	})

	t.Run("non-approved rolls back", func(t *testing.T) {
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectBegin()
		mocks.SQL.ExpectExec(clearPrefixPrimary).WithArgs(int64(3)).WillReturnResult(sqlmock.NewResult(0, 1))
		mocks.SQL.ExpectExec(setPrefixPrimary).WithArgs(int64(5), int64(3), "APPROVED").
			WillReturnResult(sqlmock.NewResult(0, 0))
		mocks.SQL.ExpectRollback()

		swapped, err := NewBrandPrefixStore().SetPrimary(ctx, 3, 5)
		require.NoError(t, err)
		assert.False(t, swapped)
	})
}

func TestBrandPrefixStore_Delete(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(deletePrefixQ).WithArgs(int64(5), int64(3), "REVOKED").
		WillReturnResult(sqlmock.NewResult(0, 1))

	deleted, err := NewBrandPrefixStore().Delete(ctx, 3, 5)
	require.NoError(t, err)
	assert.True(t, deleted)
}

func TestAdminStore_PrefixDecisions(t *testing.T) {
	reason := "header not registered"

	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(decidePrefixQuery).WithArgs("REJECTED", &reason, int64(5), "PENDING").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mocks.SQL.ExpectExec(decidePrefixQuery).WithArgs("APPROVED", nil, int64(6), "PENDING").
		WillReturnResult(sqlmock.NewResult(0, 0))
	mocks.SQL.ExpectExec(releasePrefixQuery).WithArgs(int64(5), "REVOKED").
		WillReturnResult(sqlmock.NewResult(0, 1))

	changed, err := NewAdminStore().DecidePrefix(ctx, 5, "PENDING", "REJECTED", &reason)
	require.NoError(t, err)
	assert.True(t, changed)

	changed, err = NewAdminStore().DecidePrefix(ctx, 6, "PENDING", "APPROVED", nil)
	require.NoError(t, err)
	assert.False(t, changed, "stale decision changes nothing")

	released, err := NewAdminStore().ReleasePrefix(ctx, 5)
	require.NoError(t, err)
	assert.True(t, released)
}

func TestAdminStore_ListPrefixes(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	rows := sqlmock.NewRows([]string{"id", "org_id", "prefix", "status", "is_primary", "registry", "entity_id",
		"header", "entity_name", "decision_reason", "decided_at", "created_at", "name"}).
		AddRow(5, 3, "ACMEIN", "PENDING", false, "IN-DLT", "1101", "ACMEIN", "Acme Pvt", nil, nil, time.Now(), "Acme")
	mocks.SQL.ExpectQuery(listPrefixesQuery).WithArgs("PENDING", 25, 0).WillReturnRows(rows)

	got, err := NewAdminStore().ListPrefixes(ctx, "PENDING", 25, 0)

	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "ACMEIN", got[0].Prefix)
}
