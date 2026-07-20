package services

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/models"
)

func newRuleService(t *testing.T) (*RuleService, *MockRuleStore, *MockLinkStore, *gofr.Context) {
	t.Helper()

	ctrl := gomock.NewController(t)
	rules := NewMockRuleStore(ctrl)
	links := NewMockLinkStore(ctrl)

	mockContainer, _ := container.NewMockContainer(t)
	ctx := &gofr.Context{Context: context.Background(), Container: mockContainer}

	return NewRuleService(rules, links), rules, links, ctx
}

func validRuleInput() RuleInput {
	return RuleInput{
		TargetName: "android users",
		DeviceType: &models.RuleCondition{Type: models.ConditionIs, Values: []string{"Android"}},
		URL:        "https://play.example.com",
	}
}

func TestRuleService_Create(t *testing.T) {
	svc, rules, links, ctx := newRuleService(t)
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://example.com"), nil)

	stored := models.Rule{ID: 4, OrgID: 3, LinkID: 9, TargetName: "android users"}
	rules.EXPECT().Create(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, r *models.Rule) (*models.Rule, error) {
			assert.Equal(t, int64(3), r.OrgID)
			assert.Equal(t, int64(9), r.LinkID)
			assert.Equal(t, "android users", r.TargetName)

			return &stored, nil
		})

	created, err := svc.Create(ctx, 3, 7, 9, []RuleInput{validRuleInput()})

	require.NoError(t, err)
	require.Len(t, created, 1)
	assert.Equal(t, int64(4), created[0].ID)
}

func TestRuleService_Create_MultipleKeepSubmittedOrder(t *testing.T) {
	svc, rules, links, ctx := newRuleService(t)
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://example.com"), nil)

	first := validRuleInput()
	second := validRuleInput()
	second.TargetName = "ios users"
	second.DeviceType.Values = []string{"iOS"}

	gomock.InOrder(
		rules.EXPECT().Create(gomock.Any(), matchTargetName("android users")).
			Return(&models.Rule{ID: 4, TargetName: "android users"}, nil),
		rules.EXPECT().Create(gomock.Any(), matchTargetName("ios users")).
			Return(&models.Rule{ID: 5, TargetName: "ios users"}, nil),
	)

	created, err := svc.Create(ctx, 3, 7, 9, []RuleInput{first, second})

	require.NoError(t, err)
	require.Len(t, created, 2)
	assert.Equal(t, int64(4), created[0].ID)
	assert.Equal(t, int64(5), created[1].ID)
}

// matchTargetName matches a *models.Rule by target name.
func matchTargetName(name string) gomock.Matcher {
	return gomock.Cond(func(x any) bool {
		r, ok := x.(*models.Rule)

		return ok && r.TargetName == name
	})
}

func TestRuleService_Create_Validation(t *testing.T) {
	tests := []struct {
		desc   string
		mutate func(*RuleInput)
		inputs []RuleInput
	}{
		{desc: "empty rule set", inputs: []RuleInput{}},
		{desc: "missing target name", mutate: func(r *RuleInput) { r.TargetName = " " }},
		{desc: "missing url", mutate: func(r *RuleInput) { r.URL = "" }},
		{desc: "unparseable url", mutate: func(r *RuleInput) { r.URL = "http://" }},
		{desc: "no conditions", mutate: func(r *RuleInput) { r.DeviceType = nil }},
		{desc: "bad condition operator", mutate: func(r *RuleInput) { r.DeviceType.Type = "equals" }},
		{desc: "empty condition values", mutate: func(r *RuleInput) { r.DeviceType.Values = nil }},
		{desc: "only blank condition values", mutate: func(r *RuleInput) { r.DeviceType.Values = []string{" ", ""} }},
		{desc: "location condition without values", mutate: func(r *RuleInput) {
			r.Location = &models.RuleCondition{Type: models.ConditionIs}
		}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, _, _, ctx := newRuleService(t)

			inputs := tc.inputs
			if tc.mutate != nil {
				in := validRuleInput()
				tc.mutate(&in)
				inputs = []RuleInput{in}
			}

			_, err := svc.Create(ctx, 3, 7, 9, inputs)

			require.Error(t, err)
			assertStatus(t, err, http.StatusUnprocessableEntity)
		})
	}
}

func TestRuleService_LinkVisibility(t *testing.T) {
	private := publicLink(9, "abc1234", "https://example.com")
	private.Type = models.LinkTypePrivate
	private.UserID = ptr64(7)

	tests := []struct {
		desc     string
		link     *models.Link
		viewerID int64
		wantErr  bool
	}{
		{"creator can touch a private link's rules", private, 7, false},
		{"non-creator gets 404 on a private link", private, 8, true},
		{"missing link is 404", nil, 7, true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			svc, rules, links, ctx := newRuleService(t)
			links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(tc.link, nil)

			if !tc.wantErr {
				rules.EXPECT().ListByLink(gomock.Any(), int64(3), int64(9)).Return([]models.Rule{}, nil)
			}

			_, err := svc.List(ctx, 3, tc.viewerID, 9)

			if tc.wantErr {
				require.Error(t, err)
				assertStatus(t, err, http.StatusNotFound)

				return
			}

			require.NoError(t, err)
		})
	}
}

func TestRuleService_Update(t *testing.T) {
	svc, rules, links, ctx := newRuleService(t)
	existing := &models.Rule{ID: 4, OrgID: 3, LinkID: 9, TargetName: "old"}

	rules.EXPECT().GetByID(gomock.Any(), int64(3), int64(4)).Return(existing, nil)
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://example.com"), nil)
	rules.EXPECT().Update(gomock.Any(), gomock.Any()).DoAndReturn(
		func(_ *gofr.Context, r *models.Rule) (*models.Rule, error) {
			assert.Equal(t, int64(4), r.ID, "identity comes from the existing rule")
			assert.Equal(t, int64(9), r.LinkID)
			assert.Equal(t, "android users", r.TargetName)

			return r, nil
		})

	got, err := svc.Update(ctx, 3, 7, 4, validRuleInput())

	require.NoError(t, err)
	assert.Equal(t, "android users", got.TargetName)
}

func TestRuleService_Update_MissingRule(t *testing.T) {
	svc, rules, _, ctx := newRuleService(t)
	rules.EXPECT().GetByID(gomock.Any(), int64(3), int64(4)).Return(nil, nil)

	_, err := svc.Update(ctx, 3, 7, 4, validRuleInput())

	require.Error(t, err)
	assertStatus(t, err, http.StatusNotFound)
}

func TestRuleService_Delete(t *testing.T) {
	svc, rules, links, ctx := newRuleService(t)
	rules.EXPECT().GetByID(gomock.Any(), int64(3), int64(4)).
		Return(&models.Rule{ID: 4, OrgID: 3, LinkID: 9}, nil)
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).
		Return(publicLink(9, "abc1234", "https://example.com"), nil)
	rules.EXPECT().Delete(gomock.Any(), int64(3), int64(4)).Return(nil)

	require.NoError(t, svc.Delete(ctx, 3, 7, 4))
}

func TestRuleService_Delete_PrivateLinkNonCreator(t *testing.T) {
	private := publicLink(9, "abc1234", "https://example.com")
	private.Type = models.LinkTypePrivate
	private.UserID = ptr64(7)

	svc, rules, links, ctx := newRuleService(t)
	rules.EXPECT().GetByID(gomock.Any(), int64(3), int64(4)).
		Return(&models.Rule{ID: 4, OrgID: 3, LinkID: 9}, nil)
	links.EXPECT().GetByID(gomock.Any(), int64(3), int64(9)).Return(private, nil)

	err := svc.Delete(ctx, 3, 8, 4)

	require.Error(t, err)
	assertStatus(t, err, http.StatusNotFound)
}
