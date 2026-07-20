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
	insertDomainQ        = "INSERT INTO domains (org_id, hostname, verification_token) VALUES (?, ?, ?)"
	selectDomainPrefix   = "SELECT id, org_id, hostname, verification_token, status, verified_at, is_primary, created_at FROM domains "
	selectDomainByID     = selectDomainPrefix + "WHERE id = ? AND org_id = ?"
	selectDomainByHost   = selectDomainPrefix + "WHERE hostname = ?"
	selectDomainsByOrg   = selectDomainPrefix + "WHERE org_id = ? ORDER BY id"
	selectPrimaryHost    = "SELECT hostname FROM domains WHERE org_id = ? AND is_primary = 1 AND status = ? LIMIT 1"
	updateDomainVerified = "UPDATE domains SET status = ?, verified_at = CURRENT_TIMESTAMP WHERE id = ? AND org_id = ?"
	clearPrimaryQ        = "UPDATE domains SET is_primary = 0 WHERE org_id = ?"
	setPrimaryQ          = "UPDATE domains SET is_primary = 1 WHERE id = ? AND org_id = ? AND status = ?"
	deleteDomainQ        = "DELETE FROM domains WHERE id = ? AND org_id = ?"
)

func domainColumnNames() []string {
	return []string{"id", "org_id", "hostname", "verification_token", "status", "verified_at", "is_primary", "created_at"}
}

func domainRows(id int64, hostname, status string, isPrimary bool) *sqlmock.Rows {
	return sqlmock.NewRows(domainColumnNames()).
		AddRow(id, 3, hostname, "tok123", status, nil, isPrimary, time.Now())
}

func TestDomainStore_Create(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc: "created and re-read",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertDomainQ).
					WithArgs(int64(3), "links.example.com", "tok123").
					WillReturnResult(sqlmock.NewResult(21, 1))
				m.ExpectQuery(selectDomainByID).WithArgs(int64(21), int64(3)).
					WillReturnRows(domainRows(21, "links.example.com", "PENDING", false))
			},
		},
		{
			desc: "insert fails (e.g. hostname unique-key race)",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertDomainQ).
					WithArgs(int64(3), "links.example.com", "tok123").
					WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewDomainStore().Create(ctx, 3, "links.example.com", "tok123")

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, int64(21), got.ID)
			assert.Equal(t, models.DomainStatusPending, got.Status)
			assert.False(t, got.IsPrimary)
		})
	}
}

func TestDomainStore_GetByID_ScopedToOrg(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectDomainByID).WithArgs(int64(21), int64(999)).
		WillReturnRows(sqlmock.NewRows(domainColumnNames()))

	domain, err := NewDomainStore().GetByID(ctx, 999, 21)
	require.NoError(t, err)
	assert.Nil(t, domain, "a domain from another org must not resolve")
}

func TestDomainStore_GetByHostname(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectDomainByHost).WithArgs("links.example.com").
		WillReturnRows(domainRows(21, "links.example.com", "VERIFIED", true))

	domain, err := NewDomainStore().GetByHostname(ctx, "links.example.com")
	require.NoError(t, err)
	require.NotNil(t, domain)
	assert.Equal(t, int64(3), domain.OrgID)
	assert.True(t, domain.IsPrimary)

	mocks.SQL.ExpectQuery(selectDomainByHost).WithArgs("nope.example.com").
		WillReturnRows(sqlmock.NewRows(domainColumnNames()))

	domain, err = NewDomainStore().GetByHostname(ctx, "nope.example.com")
	require.NoError(t, err)
	assert.Nil(t, domain, "unknown hostname is (nil, nil), not an error")
}

func TestDomainStore_ListByOrg(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectDomainsByOrg).WithArgs(int64(3)).
		WillReturnRows(domainRows(21, "links.example.com", "VERIFIED", true).
			AddRow(22, 3, "go.example.org", "tok456", "PENDING", nil, false, time.Now()))

	domains, err := NewDomainStore().ListByOrg(ctx, 3)
	require.NoError(t, err)
	require.Len(t, domains, 2)
	assert.Equal(t, "links.example.com", domains[0].Hostname)
	assert.Equal(t, "go.example.org", domains[1].Hostname)
}

func TestDomainStore_PrimaryVerifiedHostname(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectPrimaryHost).WithArgs(int64(3), "VERIFIED").
		WillReturnRows(sqlmock.NewRows([]string{"hostname"}).AddRow("links.example.com"))

	host, err := NewDomainStore().PrimaryVerifiedHostname(ctx, 3)
	require.NoError(t, err)
	assert.Equal(t, "links.example.com", host)

	mocks.SQL.ExpectQuery(selectPrimaryHost).WithArgs(int64(4), "VERIFIED").
		WillReturnRows(sqlmock.NewRows([]string{"hostname"}))

	host, err = NewDomainStore().PrimaryVerifiedHostname(ctx, 4)
	require.NoError(t, err)
	assert.Empty(t, host, "no primary verified domain is (\"\", nil), not an error")
}

func TestDomainStore_SetVerified(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(updateDomainVerified).WithArgs("VERIFIED", int64(21), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, NewDomainStore().SetVerified(ctx, 3, 21))
}

func TestDomainStore_SetPrimary_TransactionalSwap(t *testing.T) {
	t.Run("swap commits when the domain is verified", func(t *testing.T) {
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectBegin()
		mocks.SQL.ExpectExec(clearPrimaryQ).WithArgs(int64(3)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mocks.SQL.ExpectExec(setPrimaryQ).WithArgs(int64(21), int64(3), "VERIFIED").
			WillReturnResult(sqlmock.NewResult(0, 1))
		mocks.SQL.ExpectCommit()

		swapped, err := NewDomainStore().SetPrimary(ctx, 3, 21)
		require.NoError(t, err)
		assert.True(t, swapped)
	})

	t.Run("no verified row rolls the whole swap back", func(t *testing.T) {
		// The clear already ran inside the tx; the rollback must restore the
		// previous primary rather than leave the org with none.
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectBegin()
		mocks.SQL.ExpectExec(clearPrimaryQ).WithArgs(int64(3)).
			WillReturnResult(sqlmock.NewResult(0, 1))
		mocks.SQL.ExpectExec(setPrimaryQ).WithArgs(int64(21), int64(3), "VERIFIED").
			WillReturnResult(sqlmock.NewResult(0, 0)) // PENDING/cross-org: no row
		mocks.SQL.ExpectRollback()

		swapped, err := NewDomainStore().SetPrimary(ctx, 3, 21)
		require.NoError(t, err)
		assert.False(t, swapped)
	})

	t.Run("mid-transaction error rolls back", func(t *testing.T) {
		ctx, mocks := newTestCtx(t)
		mocks.SQL.ExpectBegin()
		mocks.SQL.ExpectExec(clearPrimaryQ).WithArgs(int64(3)).WillReturnError(errDB)
		mocks.SQL.ExpectRollback()

		_, err := NewDomainStore().SetPrimary(ctx, 3, 21)
		require.Error(t, err)
	})
}

func TestDomainStore_Delete(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(deleteDomainQ).WithArgs(int64(21), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	deleted, err := NewDomainStore().Delete(ctx, 3, 21)
	require.NoError(t, err)
	assert.True(t, deleted)

	mocks.SQL.ExpectExec(deleteDomainQ).WithArgs(int64(21), int64(999)).
		WillReturnResult(sqlmock.NewResult(0, 0))

	deleted, err = NewDomainStore().Delete(ctx, 999, 21)
	require.NoError(t, err)
	assert.False(t, deleted, "cross-org delete must touch nothing")
}
