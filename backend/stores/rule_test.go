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
	selectRulePrefix = "SELECT id, org_id, link_id, target_name, rule, created_at FROM link_rules "
	selectRuleByID   = selectRulePrefix + "WHERE id = ? AND org_id = ?"
	selectRulesLink  = selectRulePrefix + "WHERE org_id = ? AND link_id = ? ORDER BY id"

	insertRule = "INSERT INTO link_rules (org_id, link_id, target_name, rule) VALUES (?, ?, ?, ?)"
	updateRule = "UPDATE link_rules SET target_name = ?, rule = ? WHERE id = ? AND org_id = ?"
	deleteRule = "DELETE FROM link_rules WHERE id = ? AND org_id = ?"
)

var ruleColumnNames = []string{"id", "org_id", "link_id", "target_name", "rule", "created_at"}

func sampleRule() *models.Rule {
	return &models.Rule{
		OrgID:      3,
		LinkID:     9,
		TargetName: "android users",
		DeviceType: &models.RuleCondition{Type: models.ConditionIs, Values: []string{"Android"}},
		URL:        "https://play.google.com/store/apps/details?id=com.x",
	}
}

func sampleRuleJSON(t *testing.T) string {
	t.Helper()

	body, err := marshalRule(sampleRule())
	require.NoError(t, err)

	return body
}

func ruleRows(t *testing.T, id int64) *sqlmock.Rows {
	t.Helper()

	return sqlmock.NewRows(ruleColumnNames).
		AddRow(id, 3, 9, "android users", sampleRuleJSON(t), time.Now())
}

func TestRuleStore_Create(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantErr bool
	}{
		{
			desc: "created",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertRule).
					WithArgs(int64(3), int64(9), "android users", sampleRuleJSON(t)).
					WillReturnResult(sqlmock.NewResult(4, 1))
				m.ExpectQuery(selectRuleByID).WithArgs(int64(4), int64(3)).
					WillReturnRows(ruleRows(t, 4))
			},
		},
		{
			desc: "insert fails",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectExec(insertRule).
					WithArgs(int64(3), int64(9), "android users", sampleRuleJSON(t)).
					WillReturnError(errDB)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			got, err := NewRuleStore().Create(ctx, sampleRule())

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			require.NotNil(t, got)
			assert.Equal(t, int64(4), got.ID)
			require.NotNil(t, got.DeviceType, "conditions must round-trip through the rule JSON")
			assert.Equal(t, []string{"Android"}, got.DeviceType.Values)
			assert.Nil(t, got.Location)
			assert.Equal(t, "https://play.google.com/store/apps/details?id=com.x", got.URL)
		})
	}
}

func TestRuleStore_GetByID(t *testing.T) {
	tests := []struct {
		desc    string
		mock    func(m sqlmock.Sqlmock)
		wantNil bool
		wantErr bool
	}{
		{
			desc: "found",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectRuleByID).WithArgs(int64(4), int64(3)).
					WillReturnRows(ruleRows(t, 4))
			},
		},
		{
			desc: "not found is nil, not an error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectRuleByID).WithArgs(int64(4), int64(3)).
					WillReturnRows(sqlmock.NewRows(ruleColumnNames))
			},
			wantNil: true,
		},
		{
			desc: "db error",
			mock: func(m sqlmock.Sqlmock) {
				m.ExpectQuery(selectRuleByID).WithArgs(int64(4), int64(3)).WillReturnError(errDB)
			},
			wantNil: true,
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			ctx, mocks := newTestCtx(t)
			tc.mock(mocks.SQL)

			rule, err := NewRuleStore().GetByID(ctx, 3, 4)

			assert.Equal(t, tc.wantErr, err != nil)
			assert.Equal(t, tc.wantNil, rule == nil)
		})
	}
}

func TestRuleStore_ListByLink(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectRulesLink).WithArgs(int64(3), int64(9)).
		WillReturnRows(ruleRows(t, 4).AddRow(5, 3, 9, "second", sampleRuleJSON(t), time.Now()))

	rules, err := NewRuleStore().ListByLink(ctx, 3, 9)
	require.NoError(t, err)
	require.Len(t, rules, 2)
	assert.Equal(t, int64(4), rules[0].ID, "creation (evaluation) order")
	assert.Equal(t, int64(5), rules[1].ID)
}

func TestRuleStore_ListByLink_Empty(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectQuery(selectRulesLink).WithArgs(int64(3), int64(9)).
		WillReturnRows(sqlmock.NewRows(ruleColumnNames))

	rules, err := NewRuleStore().ListByLink(ctx, 3, 9)
	require.NoError(t, err)
	assert.Empty(t, rules)
	assert.NotNil(t, rules)
}

func TestRuleStore_Update(t *testing.T) {
	rule := sampleRule()
	rule.ID = 4

	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(updateRule).
		WithArgs("android users", sampleRuleJSON(t), int64(4), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mocks.SQL.ExpectQuery(selectRuleByID).WithArgs(int64(4), int64(3)).
		WillReturnRows(ruleRows(t, 4))

	got, err := NewRuleStore().Update(ctx, rule)
	require.NoError(t, err)
	require.NotNil(t, got)
	assert.Equal(t, int64(4), got.ID)
}

func TestRuleStore_Delete(t *testing.T) {
	ctx, mocks := newTestCtx(t)
	mocks.SQL.ExpectExec(deleteRule).WithArgs(int64(4), int64(3)).
		WillReturnResult(sqlmock.NewResult(0, 1))

	require.NoError(t, NewRuleStore().Delete(ctx, 3, 4))
}
