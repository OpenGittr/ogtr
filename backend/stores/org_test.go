package stores

import (
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	selectOrgByID     = "SELECT id, name, slug, auto_join_domain, created_at FROM orgs WHERE id = ?"
	selectOrgByDomain = "SELECT id, name, slug, auto_join_domain, created_at FROM orgs WHERE auto_join_domain = ? ORDER BY id LIMIT 1"
	selectSlugExists  = "SELECT 1 FROM orgs WHERE slug = ?"
	insertOrg         = "INSERT INTO orgs (name, slug, auto_join_domain) VALUES (?, ?, ?)"
	updateOrg         = "UPDATE orgs SET name = ?, auto_join_domain = ? WHERE id = ?"
)

func orgRows(id int64, name, slug string, domain *string) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"id", "name", "slug", "auto_join_domain", "created_at"}).
		AddRow(id, name, slug, domain, time.Now())
}

func TestOrgStore_CreateAndGet(t *testing.T) {
	ctx, mocks := newTestCtx(t)

	domain := "acme.com"
	mocks.SQL.ExpectExec(insertOrg).WithArgs("Acme", "acme", &domain).WillReturnResult(sqlmock.NewResult(4, 1))
	mocks.SQL.ExpectQuery(selectOrgByID).WithArgs(int64(4)).WillReturnRows(orgRows(4, "Acme", "acme", &domain))

	org, err := NewOrgStore().Create(ctx, "Acme", "acme", &domain)
	require.NoError(t, err)
	require.NotNil(t, org)
	assert.Equal(t, int64(4), org.ID)
	assert.Equal(t, "acme", org.Slug)
	require.NotNil(t, org.AutoJoinDomain)
	assert.Equal(t, "acme.com", *org.AutoJoinDomain)
}

func TestOrgStore_Create_InsertError(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(insertOrg).WithArgs("Acme", "acme", nil).WillReturnError(errDB)

	org, err := NewOrgStore().Create(ctx, "Acme", "acme", nil)
	require.Error(t, err)
	assert.Nil(t, org)
}

func TestOrgStore_GetByID_NotFound(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectOrgByID).WithArgs(int64(99)).
		WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "auto_join_domain", "created_at"}))

	org, err := NewOrgStore().GetByID(ctx, 99)
	require.NoError(t, err)
	assert.Nil(t, org)
}

func TestOrgStore_SlugExists(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		want    bool
		wantErr bool
	}{
		{
			desc: "taken",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectSlugExists).WithArgs("acme").
					WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
			},
			want: true,
		},
		{
			desc: "free",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectSlugExists).WithArgs("acme").WillReturnRows(sqlmock.NewRows([]string{"1"}))
			},
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectSlugExists).WithArgs("acme").WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			exists, err := NewOrgStore().SlugExists(ctx, "acme")

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.want, exists)
		})
	}
}

func TestOrgStore_Update(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(updateOrg).WithArgs("Renamed", nil, int64(4)).WillReturnResult(sqlmock.NewResult(0, 1))

	err := NewOrgStore().Update(ctx, 4, "Renamed", nil)
	assert.NoError(t, err)
}

func TestOrgStore_GetByAutoJoinDomain(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantNil bool
	}{
		{
			desc: "match",
			mock: func(m sqlmock.Sqlmock) {
				d := "corp.io"
				m.ExpectQuery(selectOrgByDomain).WithArgs("corp.io").WillReturnRows(orgRows(2, "Corp", "corp", &d))
			},
		},
		{
			desc: "no match is nil, not an error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectOrgByDomain).WithArgs("corp.io").
					WillReturnRows(sqlmock.NewRows([]string{"id", "name", "slug", "auto_join_domain", "created_at"}))
			},
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			org, err := NewOrgStore().GetByAutoJoinDomain(ctx, "corp.io")

			require.NoError(t, err)
			assert.Equal(t, tc.wantNil, org == nil)
		})
	}
}
