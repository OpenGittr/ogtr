package stores

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	selectRole      = "SELECT role FROM org_members WHERE org_id = ? AND user_id = ?"
	insertMember    = "INSERT INTO org_members (org_id, user_id, role) VALUES (?, ?, ?)"
	deleteMember    = "DELETE FROM org_members WHERE org_id = ? AND user_id = ?"
	countOwners     = "SELECT COUNT(*) FROM org_members WHERE org_id = ? AND role = ?"
	selectUserOrgs  = "SELECT o.id, o.name, o.slug, m.role\n\t\t FROM org_members m JOIN orgs o ON o.id = m.org_id\n\t\t WHERE m.user_id = ? ORDER BY m.created_at, o.id"
	selectOrgMember = "SELECT u.id, u.name, u.email, m.role, m.created_at\n\t\t FROM org_members m JOIN users u ON u.id = m.user_id\n\t\t WHERE m.org_id = ? ORDER BY m.created_at, u.id"
)

func TestMemberStore_GetRole(t *testing.T) {
	tests := []struct {
		desc     string
		mock     func(m sqlmock.Sqlmock)
		wantRole string
		wantErr  bool
	}{
		{
			desc: "member",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectRole).WithArgs(int64(1), int64(2)).
					WillReturnRows(sqlmock.NewRows([]string{"role"}).AddRow("OWNER"))
			},
			wantRole: "OWNER",
		},
		{
			desc: "not a member is empty role, not an error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectRole).WithArgs(int64(1), int64(2)).
					WillReturnRows(sqlmock.NewRows([]string{"role"}))
			},
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectRole).WithArgs(int64(1), int64(2)).WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			role, err := NewMemberStore().GetRole(ctx, 1, 2)

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.wantRole, role)
		})
	}
}

func TestMemberStore_AddAndRemove(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	mocks.SQL.ExpectExec(insertMember).WithArgs(int64(1), int64(2), "MEMBER").
		WillReturnResult(sqlmock.NewResult(0, 1))
	mocks.SQL.ExpectExec(deleteMember).WithArgs(int64(1), int64(2)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, NewMemberStore().Add(ctx, 1, 2, "MEMBER"))
	require.NoError(t, NewMemberStore().Remove(ctx, 1, 2))
}

func TestMemberStore_CountOwners(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(countOwners).WithArgs(int64(1), "OWNER").
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(2))

	n, err := NewMemberStore().CountOwners(ctx, 1)
	require.NoError(t, err)
	assert.Equal(t, 2, n)
}

func TestMemberStore_ListOrgsForUser(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantLen int
		wantErr bool
	}{
		{
			desc: "two orgs",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectUserOrgs).WithArgs(int64(2)).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "role"}).
						AddRow(1, "Acme", "acme", "OWNER").
						AddRow(3, "Corp", "corp", "MEMBER"))
			},
			wantLen: 2,
		},
		{
			desc: "no orgs is an empty slice",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectUserOrgs).WithArgs(int64(2)).
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "role"}))
			},
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectUserOrgs).WithArgs(int64(2)).WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			orgs, err := NewMemberStore().ListOrgsForUser(ctx, 2)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, orgs)
			assert.Len(t, orgs, tc.wantLen)
		})
	}
}

func TestMemberStore_ListMembers(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectOrgMember).WithArgs(int64(1)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "email", "role", "created_at"}).
			AddRow(2, "A", "a@b.co", "OWNER", time.Now()).
			AddRow(5, "B", "b@b.co", "MEMBER", time.Now()))

	members, err := NewMemberStore().ListMembers(ctx, 1)
	require.NoError(t, err)
	require.Len(t, members, 2)
	assert.Equal(t, int64(2), members[0].UserID)
	assert.Equal(t, "MEMBER", members[1].Role)
}
