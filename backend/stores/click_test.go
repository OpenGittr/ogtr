package stores

import (
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/opengittr/ogtr/backend/models"
)

func TestClickStore_Insert(t *testing.T) {
	full := &models.Click{
		OrgID: 3, LinkID: 9,
		UTMSource: "google", UTMMedium: "referrer by Mobile", UTMCampaign: "",
		DeviceType: "Mobile", MobileOS: "iOS", Browser: "Safari",
		Referrer: "https://google.com", IP: "203.0.113.9",
		City: "", Region: "Karnataka", Country: "India",
		IsDeeplink: false, TargetMatched: false, CustomTagID: "tag-1",
	}

	tests := []struct {
		desc    string
		click   *models.Click
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc:  "inserted with empty strings as NULL",
			click: full,
			mock: func(m sqlmock.Sqlmock) {
				// The column list is fixed in the statement itself (INV-2);
				// matching the exact SQL asserts the allowlist structurally.
				m.ExpectExec(insertClick).
					WithArgs(int64(3), int64(9), "google", "referrer by Mobile", nil,
						"Mobile", "iOS", "Safari", "https://google.com", "203.0.113.9",
						nil, "India", "Karnataka",
						false, false, "tag-1").
					WillReturnResult(sqlmock.NewResult(1, 1))
			},
		},
		{
			desc:  "db error",
			click: full,
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertClick).
					WithArgs(int64(3), int64(9), "google", "referrer by Mobile", nil,
						"Mobile", "iOS", "Safari", "https://google.com", "203.0.113.9",
						nil, "India", "Karnataka",
						false, false, "tag-1").
					WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			err := NewClickStore().Insert(ctx, tc.click)

			assert.Equal(t, tc.wantErr, err != nil)
		})
	}
}

func TestNullable(t *testing.T) {
	assert.Nil(t, nullable(""))
	require.Equal(t, "x", nullable("x"))
}
