package stores

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	insertAPIKey       = "INSERT INTO api_keys (org_id, name, key_hash, key_hint) VALUES (?, ?, ?, ?)"
	selectAPIKeyPrefix = "SELECT id, org_id, name, key_hint, status, created_at, last_used_at FROM api_keys "
	selectAPIKeyByID   = selectAPIKeyPrefix + "WHERE id = ? AND org_id = ?"
	selectAPIKeyByHash = selectAPIKeyPrefix + "WHERE key_hash = ?"
	selectAPIKeyList   = selectAPIKeyPrefix + "WHERE org_id = ? ORDER BY created_at DESC, id DESC"
	updateAPIKeyStatus = "UPDATE api_keys SET status = ? WHERE id = ? AND org_id = ?"
	updateAPIKeyUsed   = "UPDATE api_keys SET last_used_at = CURRENT_TIMESTAMP WHERE id = ?"
)

func apiKeyColumnNames() []string {
	return []string{"id", "org_id", "name", "key_hint", "status", "created_at", "last_used_at"}
}

func apiKeyRows(id int64, name, status string) *sqlmock.Rows {
	return sqlmock.NewRows(apiKeyColumnNames()).
		AddRow(id, 3, name, "slk_Ab12Cd34", status, time.Now(), nil)
}

func TestAPIKeyStore_Create(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc: "created; select never touches key_hash",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertAPIKey).
					WithArgs(int64(3), "ci key", "deadbeef", "slk_Ab12Cd34").
					WillReturnResult(sqlmock.NewResult(11, 1))
				m.ExpectQuery(selectAPIKeyByID).WithArgs(int64(11), int64(3)).
					WillReturnRows(apiKeyRows(11, "ci key", "ENABLED"))
			},
		},
		{
			desc: "insert fails",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertAPIKey).
					WithArgs(int64(3), "ci key", "deadbeef", "slk_Ab12Cd34").
					WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewAPIKeyStore().Create(ctx, 3, "ci key", "deadbeef", "slk_Ab12Cd34")

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, int64(11), got.ID)
			assert.Equal(t, "slk_Ab12Cd34", got.KeyHint)
			assert.Equal(t, "ENABLED", got.Status)
			assert.Nil(t, got.LastUsedAt)
		})
	}
}

func TestAPIKeyStore_GetByID_ScopedToOrg(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectAPIKeyByID).WithArgs(int64(11), int64(999)).
		WillReturnRows(sqlmock.NewRows(apiKeyColumnNames()))

	key, err := NewAPIKeyStore().GetByID(ctx, 999, 11)
	require.NoError(t, err)
	assert.Nil(t, key, "a key from another org must not resolve")
}

func TestAPIKeyStore_GetByHash(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantNil bool
		wantErr bool
	}{
		{
			desc: "found",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectAPIKeyByHash).WithArgs("deadbeef").
					WillReturnRows(apiKeyRows(11, "ci key", "ENABLED"))
			},
		},
		{
			desc: "unknown hash is nil, not an error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectAPIKeyByHash).WithArgs("deadbeef").
					WillReturnRows(sqlmock.NewRows(apiKeyColumnNames()))
			},
			wantNil: true,
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectAPIKeyByHash).WithArgs("deadbeef").WillReturnError(errDB)
			},
			wantNil: true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			key, err := NewAPIKeyStore().GetByHash(ctx, "deadbeef")

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.wantNil, key == nil)
		})
	}
}

func TestAPIKeyStore_List(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	used := time.Now()
	mocks.SQL.ExpectQuery(selectAPIKeyList).WithArgs(int64(3)).
		WillReturnRows(apiKeyRows(12, "new key", "ENABLED").
			AddRow(11, 3, "old key", "slk_Zz98Yy76", "DISABLED", time.Now(), used))

	keys, err := NewAPIKeyStore().List(ctx, 3)
	require.NoError(t, err)
	require.Len(t, keys, 2)
	assert.Equal(t, "new key", keys[0].Name)
	assert.Equal(t, "DISABLED", keys[1].Status)
	require.NotNil(t, keys[1].LastUsedAt)
}

func TestAPIKeyStore_List_Empty(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectAPIKeyList).WithArgs(int64(3)).
		WillReturnRows(sqlmock.NewRows(apiKeyColumnNames()))

	keys, err := NewAPIKeyStore().List(ctx, 3)
	require.NoError(t, err)
	assert.Empty(t, keys)
	assert.NotNil(t, keys)
}

func TestAPIKeyStore_Disable(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(updateAPIKeyStatus).WithArgs("DISABLED", int64(11), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, NewAPIKeyStore().Disable(ctx, 3, 11))
}

func TestAPIKeyStore_TouchLastUsed(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(updateAPIKeyUsed).WithArgs(int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, NewAPIKeyStore().TouchLastUsed(ctx, 11))
}
