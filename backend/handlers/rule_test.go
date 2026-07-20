package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/models"
	"github.com/opengittr/ogtr/backend/services"
)

func newRuleHandler(t *testing.T) (*RuleHandler, *MockRuleService, *MockCityIndex) {
	t.Helper()

	ctrl := gomock.NewController(t)
	rules := NewMockRuleService(ctrl)
	cities := NewMockCityIndex(ctrl)

	return NewRuleHandler(rules, cities), rules, cities
}

func TestRuleHandler_Create(t *testing.T) {
	created := []models.Rule{{ID: 4, TargetName: "android users"}}

	tests := []struct {
		desc    string
		body    string
		orgless bool
		setup   func(svc *MockRuleService)
		wantErr bool
	}{
		{
			desc: "creates rules",
			body: `[{"target_name":"android users","device_type":{"type":"is","values":["Android"]},"url":"https://x.co"}]`,
			setup: func(svc *MockRuleService) {
				svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), int64(9),
					[]services.RuleInput{{
						TargetName: "android users",
						DeviceType: &models.RuleCondition{Type: "is", Values: []string{"Android"}},
						URL:        "https://x.co",
					}}).Return(created, nil)
			},
		},
		{desc: "invalid body", body: `{`, wantErr: true},
		{desc: "object instead of array", body: `{"target_name":"x"}`, wantErr: true},
		{desc: "org-less token rejected", body: `[]`, orgless: true, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc, _ := newRuleHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			claims := orgOwnerClaims()
			if tc.orgless {
				claims = orglessClaims()
			}

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/links/9/rules", tc.body, claims,
				map[string]string{"id": "9"})

			got, err := h.Create(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, created, got)
		})
	}
}

func TestRuleHandler_List(t *testing.T) {
	rules := []models.Rule{{ID: 4}, {ID: 5}}

	h, svc, _ := newRuleHandler(t)
	svc.EXPECT().List(gomock.Any(), int64(3), int64(7), int64(9)).Return(rules, nil)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/links/9/rules", "", orgOwnerClaims(),
		map[string]string{"id": "9"})

	got, err := h.List(ctx)

	require.NoError(t, err)
	assert.Equal(t, rules, got)
}

func TestRuleHandler_List_BadID(t *testing.T) {
	h, _, _ := newRuleHandler(t)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/links/x/rules", "", orgOwnerClaims(),
		map[string]string{"id": "x"})

	_, err := h.List(ctx)
	require.Error(t, err)
}

func TestRuleHandler_Update(t *testing.T) {
	updated := &models.Rule{ID: 4, TargetName: "renamed"}

	tests := []struct {
		desc    string
		body    string
		setup   func(svc *MockRuleService)
		wantErr bool
	}{
		{
			desc: "updates the rule",
			body: `{"target_name":"renamed","location":{"type":"is_not","values":["Delhi"]},"url":"https://x.co"}`,
			setup: func(svc *MockRuleService) {
				svc.EXPECT().Update(gomock.Any(), int64(3), int64(7), int64(4),
					services.RuleInput{
						TargetName: "renamed",
						Location:   &models.RuleCondition{Type: "is_not", Values: []string{"Delhi"}},
						URL:        "https://x.co",
					}).Return(updated, nil)
			},
		},
		{desc: "invalid body", body: `{`, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc, _ := newRuleHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			ctx := newTestCtx(t, http.MethodPut, "/api/v1/rules/4", tc.body, orgOwnerClaims(),
				map[string]string{"ruleId": "4"})

			got, err := h.Update(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, updated, got)
		})
	}
}

func TestRuleHandler_Delete(t *testing.T) {
	tests := []struct {
		desc    string
		setup   func(svc *MockRuleService)
		wantErr bool
	}{
		{
			desc: "deleted",
			setup: func(svc *MockRuleService) {
				svc.EXPECT().Delete(gomock.Any(), int64(3), int64(7), int64(4)).Return(nil)
			},
		},
		{
			desc: "service error propagates",
			setup: func(svc *MockRuleService) {
				svc.EXPECT().Delete(gomock.Any(), int64(3), int64(7), int64(4)).Return(assert.AnError)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc, _ := newRuleHandler(t)
			tc.setup(svc)

			ctx := newTestCtx(t, http.MethodDelete, "/api/v1/rules/4", "", orgOwnerClaims(),
				map[string]string{"ruleId": "4"})

			got, err := h.Delete(ctx)

			if tc.wantErr {
				require.Error(t, err)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, map[string]bool{"deleted": true}, got)
		})
	}
}

func TestRuleHandler_Cities(t *testing.T) {
	tests := []struct {
		desc       string
		query      string
		setup      func(cities *MockCityIndex)
		want       []string
		wantErr    bool
		wantStatus int
	}{
		{
			desc:  "prefix search",
			query: "?q=del",
			setup: func(cities *MockCityIndex) {
				cities.EXPECT().Enabled().Return(true)
				cities.EXPECT().Search("del", 20).Return([]string{"Delhi", "Delft"})
			},
			want: []string{"Delhi", "Delft"},
		},
		{
			desc:  "dataset not configured is 501",
			query: "?q=del",
			setup: func(cities *MockCityIndex) {
				cities.EXPECT().Enabled().Return(false)
			},
			wantErr:    true,
			wantStatus: http.StatusNotImplemented,
		},
		{
			desc:  "missing q",
			query: "",
			setup: func(cities *MockCityIndex) {
				cities.EXPECT().Enabled().Return(true)
			},
			wantErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, _, cities := newRuleHandler(t)
			if tc.setup != nil {
				tc.setup(cities)
			}

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/cities"+tc.query, "", orgOwnerClaims(), nil)

			got, err := h.Cities(ctx)

			if tc.wantErr {
				require.Error(t, err)

				if tc.wantStatus != 0 {
					sc, ok := err.(interface{ StatusCode() int })
					require.True(t, ok)
					assert.Equal(t, tc.wantStatus, sc.StatusCode())
				}

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRuleHandler_Cities_Orgless(t *testing.T) {
	h, _, _ := newRuleHandler(t)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/cities?q=del", "", orglessClaims(), nil)

	_, err := h.Cities(ctx)
	require.Error(t, err)
}
