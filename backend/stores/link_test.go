package stores

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opengittr/ogtr/backend/models"
)

const (
	selectLinkPrefix = "SELECT id, org_id, user_id, api_key_id, code, destination_url, type, status, " +
		"utm_source, utm_medium, utm_campaign, deeplink_config, visits, last_visit_at, created_at FROM links "

	selectLinkByID          = selectLinkPrefix + "WHERE id = ? AND org_id = ?"
	selectLinkByCode        = selectLinkPrefix + "WHERE code = ?"
	selectLinkByDestination = selectLinkPrefix +
		"WHERE org_id = ? AND (type = 'PUBLIC' OR user_id = ?) AND destination_url = ? ORDER BY id LIMIT 1"
	selectLinkPage = selectLinkPrefix +
		"WHERE org_id = ? AND (type = 'PUBLIC' OR user_id = ?) ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?"
	selectLinkCount = "SELECT COUNT(*) FROM links WHERE org_id = ? AND (type = 'PUBLIC' OR user_id = ?)"
	selectLinkCode  = "SELECT 1 FROM links WHERE code = ?"

	updateLinkCode     = "UPDATE links SET code = ? WHERE id = ? AND org_id = ?"
	updateLinkDeeplink = "UPDATE links SET deeplink_config = ? WHERE id = ? AND org_id = ?"
	updateLinkVisit    = "UPDATE links SET visits = visits + 1, last_visit_at = CURRENT_TIMESTAMP WHERE id = ?"
)

var linkColumnNames = []string{
	"id", "org_id", "user_id", "api_key_id", "code", "destination_url", "type", "status",
	"utm_source", "utm_medium", "utm_campaign", "deeplink_config", "visits", "last_visit_at", "created_at",
}

func linkRows(id int64, code, dest string) *sqlmock.Rows {
	return sqlmock.NewRows(linkColumnNames).
		AddRow(id, 3, 7, nil, code, dest, "PUBLIC", "ACTIVE", nil, nil, nil, nil, 5, nil, time.Now())
}

func emptyLinkRows() *sqlmock.Rows { return sqlmock.NewRows(linkColumnNames) }

func TestLinkStore_Create(t *testing.T) {
	link := &models.Link{OrgID: 3, UserID: ptr64(7), Code: "abc1234", DestinationURL: "https://x.co", Type: "PUBLIC"}

	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc: "created",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertLink).
					WithArgs(int64(3), int64(7), nil, "abc1234", "https://x.co", "PUBLIC", nil, nil, nil).
					WillReturnResult(sqlmock.NewResult(9, 1))
				m.ExpectQuery(selectLinkByID).WithArgs(int64(9), int64(3)).
					WillReturnRows(linkRows(9, "abc1234", "https://x.co"))
			},
		},
		{
			desc: "insert fails",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertLink).
					WithArgs(int64(3), int64(7), nil, "abc1234", "https://x.co", "PUBLIC", nil, nil, nil).
					WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewLinkStore().Create(ctx, link)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, int64(9), got.ID)
			assert.Equal(t, "abc1234", got.Code)
		})
	}
}

func TestLinkStore_GetByID(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectLinkByID).WithArgs(int64(9), int64(3)).
		WillReturnRows(linkRows(9, "abc1234", "https://x.co"))

	link, err := NewLinkStore().GetByID(ctx, 3, 9)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Equal(t, "https://x.co", link.DestinationURL)
}

func TestLinkStore_GetByCode(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantNil bool
		wantErr bool
	}{
		{
			desc: "found",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectLinkByCode).WithArgs("abc1234").
					WillReturnRows(linkRows(9, "abc1234", "https://x.co"))
			},
		},
		{
			desc: "not found is nil, not an error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectLinkByCode).WithArgs("abc1234").WillReturnRows(emptyLinkRows())
			},
			wantNil: true,
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectLinkByCode).WithArgs("abc1234").WillReturnError(errDB)
			},
			wantNil: true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			link, err := NewLinkStore().GetByCode(ctx, "abc1234")

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.wantNil, link == nil)
		})
	}
}

func TestLinkStore_FindByDestination(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantNil bool
	}{
		{
			desc: "found",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectLinkByDestination).WithArgs(int64(3), int64(7), "https://x.co").
					WillReturnRows(linkRows(9, "abc1234", "https://x.co"))
			},
		},
		{
			desc: "no match is nil, not an error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectLinkByDestination).WithArgs(int64(3), int64(7), "https://x.co").
					WillReturnRows(emptyLinkRows())
			},
			wantNil: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			link, err := NewLinkStore().FindByDestination(ctx, 3, 7, "https://x.co")

			require.NoError(t, err)
			assert.Equal(t, tc.wantNil, link == nil)
		})
	}
}

func TestLinkStore_CodeExists(t *testing.T) {
	tests := []struct {
		desc string
		mock func(m sqlmock.Sqlmock)
		want bool
	}{
		{
			desc: "taken",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectLinkCode).WithArgs("abc1234").
					WillReturnRows(sqlmock.NewRows([]string{"1"}).AddRow(1))
			},
			want: true,
		},
		{
			desc: "free",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectLinkCode).WithArgs("abc1234").
					WillReturnRows(sqlmock.NewRows([]string{"1"}))
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			taken, err := NewLinkStore().CodeExists(ctx, "abc1234")

			require.NoError(t, err)
			assert.Equal(t, tc.want, taken)
		})
	}
}

func TestLinkStore_List(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectLinkPage).WithArgs(int64(3), int64(7), 10, 10).
		WillReturnRows(linkRows(9, "abc1234", "https://x.co").
			AddRow(8, 3, 7, nil, "zzz9999", "https://y.co", "PRIVATE", "ACTIVE", nil, nil, nil, nil, 0, nil, time.Now()))

	links, err := NewLinkStore().List(ctx, 3, 7, 10, 10)
	require.NoError(t, err)
	require.Len(t, links, 2)
	assert.Equal(t, "abc1234", links[0].Code)
	assert.Equal(t, "PRIVATE", links[1].Type)
}

func TestLinkStore_List_Empty(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectLinkPage).WithArgs(int64(3), int64(7), 10, 0).
		WillReturnRows(emptyLinkRows())

	links, err := NewLinkStore().List(ctx, 3, 7, 10, 0)
	require.NoError(t, err)
	assert.Empty(t, links)
	assert.NotNil(t, links)
}

func TestLinkStore_Count(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectLinkCount).WithArgs(int64(3), int64(7)).
		WillReturnRows(sqlmock.NewRows([]string{"count"}).AddRow(23))

	n, err := NewLinkStore().Count(ctx, 3, 7)
	require.NoError(t, err)
	assert.Equal(t, int64(23), n)
}

func TestLinkStore_UpdateCode(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(updateLinkCode).WithArgs("my-brand", int64(9), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, NewLinkStore().UpdateCode(ctx, 3, 9, "my-brand"))
}

func TestLinkStore_ScanNullUserID(t *testing.T) {
	// API-key-created links have user_id NULL and api_key_id set — both must
	// round-trip as pointers (phase 6).
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectLinkByID).WithArgs(int64(9), int64(3)).
		WillReturnRows(sqlmock.NewRows(linkColumnNames).
			AddRow(9, 3, nil, 11, "abc1234", "https://x.co", "PUBLIC", "ACTIVE", nil, nil, nil, nil, 0, nil, time.Now()))

	link, err := NewLinkStore().GetByID(ctx, 3, 9)
	require.NoError(t, err)
	require.NotNil(t, link)
	assert.Nil(t, link.UserID)
	require.NotNil(t, link.APIKeyID)
	assert.Equal(t, int64(11), *link.APIKeyID)
}

func TestLinkStore_ScanDeeplinkConfig(t *testing.T) {
	raw := `{"android":{"intent":"open","package":"com.x","scheme":"x","fallback_url":"https://x.co"},` +
		`"ios":{"intent":"https://apps.x.co"}}`

	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectLinkByID).WithArgs(int64(9), int64(3)).
		WillReturnRows(sqlmock.NewRows(linkColumnNames).
			AddRow(9, 3, 7, nil, "abc1234", "https://x.co", "PUBLIC", "ACTIVE", nil, nil, nil, []byte(raw), 5, nil, time.Now()))

	link, err := NewLinkStore().GetByID(ctx, 3, 9)
	require.NoError(t, err)
	require.NotNil(t, link)
	require.NotNil(t, link.Deeplink)
	require.NotNil(t, link.Deeplink.Android)
	require.NotNil(t, link.Deeplink.IOS)
	assert.Equal(t, "com.x", link.Deeplink.Android.Package)
	assert.Equal(t, "https://apps.x.co", link.Deeplink.IOS.Intent)
}

func TestLinkStore_UpdateDeeplink(t *testing.T) {
	cfg := &models.DeeplinkConfig{
		Android: &models.AndroidDeeplink{
			Intent: "open", Package: "com.x", Scheme: "x", FallbackURL: "https://x.co",
		},
	}
	cfgJSON, _ := json.Marshal(cfg)

	tests := []struct {
		desc string
		cfg  *models.DeeplinkConfig
		arg  any
	}{
		{"set stores json as string", cfg, string(cfgJSON)},
		{"nil clears to NULL", nil, nil},
		{"empty config clears to NULL", &models.DeeplinkConfig{}, nil},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			mocks.SQL.ExpectExec(updateLinkDeeplink).WithArgs(tc.arg, int64(9), int64(3)).
				WillReturnResult(sqlmock.NewResult(0, 1))

			require.NoError(t, NewLinkStore().UpdateDeeplink(ctx, 3, 9, tc.cfg))
		})
	}
}

func TestLinkStore_RecordVisit(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc: "recorded",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(updateLinkVisit).WithArgs(int64(9)).WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(updateLinkVisit).WithArgs(int64(9)).WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			err := NewLinkStore().RecordVisit(ctx, 9)

			assert.Equal(t, tc.wantErr, err != nil)
		})
	}
}

const (
	updateLinkDestination = "UPDATE links SET destination_url = ?, utm_source = ?, utm_medium = ?, " +
		"utm_campaign = ? WHERE id = ? AND org_id = ?"
	insertLinkEditSQL = "INSERT INTO link_edits (org_id, link_id, user_id, old_url, new_url) " +
		"VALUES (?, ?, ?, ?, ?)"
)

func TestLinkStore_UpdateDestination(t *testing.T) {
	source := "tw"

	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc: "updated with utm source set and the rest cleared",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(updateLinkDestination).
					WithArgs("https://new.example.com", &source, nil, nil, int64(9), int64(3)).
					WillReturnResult(sqlmock.NewResult(0, 1))
			},
		},
		{
			desc: "update fails",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(updateLinkDestination).
					WithArgs("https://new.example.com", &source, nil, nil, int64(9), int64(3)).
					WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			err := NewLinkStore().UpdateDestination(ctx, 3, 9, "https://new.example.com", &source, nil, nil)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestLinkStore_InsertEdit(t *testing.T) {
	edit := &models.LinkEdit{OrgID: 3, LinkID: 9, UserID: 7,
		OldURL: "https://old.example.com", NewURL: "https://new.example.com"}

	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc: "audit row inserted",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertLinkEditSQL).
					WithArgs(int64(3), int64(9), int64(7), "https://old.example.com", "https://new.example.com").
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
		},
		{
			desc: "insert fails",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertLinkEditSQL).
					WithArgs(int64(3), int64(9), int64(7), "https://old.example.com", "https://new.example.com").
					WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			err := NewLinkStore().InsertEdit(ctx, edit)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
		})
	}
}
