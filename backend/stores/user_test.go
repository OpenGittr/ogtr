package stores

import (
	"errors"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errDB = errors.New("db down")

const (
	selectUserByEmail = "SELECT id, name, email, status, created_at FROM users WHERE email = ?"
	selectUserByID    = "SELECT id, name, email, status, created_at FROM users WHERE id = ?"
	insertUser        = "INSERT INTO users (name, email, status) VALUES (?, ?, ?)"
)

func userRows(id int64, name, email string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "email", "status", "created_at"}).
		AddRow(id, name, email, "ENABLED", time.Now())
}

func emptyUserRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "email", "status", "created_at"})
}

func TestUserStore_GetByEmail(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantNil bool
		wantErr bool
	}{
		{
			desc: "found",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectUserByEmail).WithArgs("a@b.co").WillReturnRows(userRows(1, "A", "a@b.co"))
			},
		},
		{
			desc: "not found is nil, not an error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectUserByEmail).WithArgs("a@b.co").WillReturnRows(emptyUserRows())
			},
			wantNil: true,
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectUserByEmail).WithArgs("a@b.co").WillReturnError(errDB)
			},
			wantNil: true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			user, err := NewUserStore().GetByEmail(ctx, "a@b.co")

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.wantNil, user == nil)
		})
	}
}

func TestUserStore_GetByID(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectUserByID).WithArgs(int64(3)).WillReturnRows(userRows(3, "C", "c@d.co"))

	user, err := NewUserStore().GetByID(ctx, 3)
	require.NoError(t, err)
	require.NotNil(t, user)
	assert.Equal(t, int64(3), user.ID)
	assert.Equal(t, "c@d.co", user.Email)
}

func TestUserStore_TouchLastActive(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc: "stale row is touched",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(touchLastActiveQuery).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			desc: "recently-touched row matches nothing and is still success",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(touchLastActiveQuery).WithArgs(int64(7)).WillReturnResult(sqlmock.NewResult(0, 0))
			},
		},
		{
			desc: "db error propagates",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(touchLastActiveQuery).WithArgs(int64(7)).WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			err := NewUserStore().TouchLastActive(ctx, 7)

			assert.Equal(t, tc.wantErr, err != nil)
		})
	}
}

func TestUserStore_Create(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc: "created",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertUser).WithArgs("New", "new@x.co", "ENABLED").WillReturnResult(sqlmock.NewResult(9, 1))
				m.ExpectQuery(selectUserByID).WithArgs(int64(9)).WillReturnRows(userRows(9, "New", "new@x.co"))
			},
		},
		{
			desc: "insert fails",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertUser).WithArgs("New", "new@x.co", "ENABLED").WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			user, err := NewUserStore().Create(ctx, "New", "new@x.co")

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, user)
			assert.Equal(t, int64(9), user.ID)
		})
	}
}
