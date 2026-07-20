package stores

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	insertInvite         = "INSERT INTO invites (org_id, email, invited_by, status) VALUES (?, ?, ?, ?)"
	selectInviteByID     = "SELECT id, org_id, email, invited_by, status, created_at FROM invites WHERE id = ? AND org_id = ?"
	selectPendingInvites = "SELECT id, org_id, email, invited_by, status, created_at FROM invites WHERE org_id = ? AND status = ? ORDER BY created_at, id"
	selectHasPending     = "SELECT 1 FROM invites WHERE org_id = ? AND email = ? AND status = ? LIMIT 1"
	selectPendingByEmail = "SELECT id, org_id, email, invited_by, status, created_at FROM invites WHERE email = ? AND status = ? ORDER BY created_at, id"
	updateInviteStatus   = "UPDATE invites SET status = ? WHERE id = ?"
)

func inviteRowCols() []string {
	return []string{"id", "org_id", "email", "invited_by", "status", "created_at"}
}

func inviteRows(id, orgID int64, email, status string) *sqlmock.Rows {
	return sqlmock.NewRows(inviteRowCols()).AddRow(id, orgID, email, 1, status, time.Now())
}

func TestInviteStore_Create(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	mocks.SQL.ExpectExec(insertInvite).WithArgs(int64(1), "x@y.co", int64(7), "PENDING").
		WillReturnResult(sqlmock.NewResult(11, 1))
	mocks.SQL.ExpectQuery(selectInviteByID).WithArgs(int64(11), int64(1)).
		WillReturnRows(inviteRows(11, 1, "x@y.co", "PENDING"))

	invite, err := NewInviteStore().Create(ctx, 1, "x@y.co", 7)
	require.NoError(t, err)
	require.NotNil(t, invite)
	assert.Equal(t, int64(11), invite.ID)
	assert.Equal(t, "PENDING", invite.Status)
}

func TestInviteStore_GetByID_ScopedToOrg(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectInviteByID).WithArgs(int64(11), int64(999)).
		WillReturnRows(sqlmock.NewRows(inviteRowCols()))

	invite, err := NewInviteStore().GetByID(ctx, 999, 11)
	require.NoError(t, err)
	assert.Nil(t, invite, "an invite from another org must not resolve")
}

func TestInviteStore_ListPending(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectPendingInvites).WithArgs(int64(1), "PENDING").
		WillReturnRows(inviteRows(11, 1, "x@y.co", "PENDING"))

	invites, err := NewInviteStore().ListPending(ctx, 1)
	require.NoError(t, err)
	require.Len(t, invites, 1)
	assert.Equal(t, "x@y.co", invites[0].Email)
}

func TestInviteStore_HasPending(t *testing.T) {
	tests := []struct {
		desc string
		mock func(m sqlmock.Sqlmock)
		want bool
	}{
		{
			desc: "pending exists",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectHasPending).WithArgs(int64(1), "x@y.co", "PENDING").
					WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
			},
			want: true,
		},
		{
			desc: "none",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectHasPending).WithArgs(int64(1), "x@y.co", "PENDING").
					WillReturnRows(sqlmock.NewRows([]string{"1"}))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewInviteStore().HasPending(ctx, 1, "x@y.co")
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestInviteStore_PendingForEmail(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectPendingByEmail).WithArgs("x@y.co", "PENDING").
		WillReturnRows(inviteRows(11, 1, "x@y.co", "PENDING"))

	invites, err := NewInviteStore().PendingForEmail(ctx, "x@y.co")
	require.NoError(t, err)
	require.Len(t, invites, 1)
	assert.Equal(t, int64(1), invites[0].OrgID)
}

func TestInviteStore_SetStatus(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(updateInviteStatus).WithArgs("REVOKED", int64(11)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	assert.NoError(t, NewInviteStore().SetStatus(ctx, 11, "REVOKED"))
}
