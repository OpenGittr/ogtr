package handlers

import (
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/models"
)

func newDomainHandler(t *testing.T) (*DomainHandler, *MockDomainService) {
	t.Helper()

	svc := NewMockDomainService(gomock.NewController(t))

	return NewDomainHandler(svc), svc
}

func testDomain(status string) *models.Domain {
	return &models.Domain{
		ID: 21, OrgID: 3, Hostname: "links.example.com", Status: status,
		TXTRecordName:  "_ogtr-verify.links.example.com",
		TXTRecordValue: "tok1234567890tok1234567890tok123",
	}
}

func TestDomainHandler_Create(t *testing.T) {
	created := testDomain(models.DomainStatusPending)

	tests := []struct {
		desc    string
		body    string
		orgless bool
		setup   func(svc *MockDomainService)
		wantErr bool
	}{
		{
			desc: "created with TXT instructions",
			body: `{"hostname":"links.example.com"}`,
			setup: func(svc *MockDomainService) {
				svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), "links.example.com").
					Return(created, nil)
			},
		},
		{desc: "missing hostname", body: `{}`, wantErr: true},
		{desc: "invalid body", body: `{`, wantErr: true},
		{desc: "org-less token rejected", body: `{"hostname":"links.example.com"}`, orgless: true, wantErr: true},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newDomainHandler(t)
			if tc.setup != nil {
				tc.setup(svc)
			}

			claims := orgOwnerClaims()
			if tc.orgless {
				claims = orglessClaims()
			}

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/domains", tc.body, claims, nil)

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

// TestDomainHandler_UntypedNilOnError pins the gofr 206 regression for every
// domain endpoint: on a service error the handler must return exactly nil,
// never the service's typed nil.
func TestDomainHandler_UntypedNilOnError(t *testing.T) {
	tests := []struct {
		desc string
		call func(h *DomainHandler, svc *MockDomainService) (any, error)
	}{
		{"create", func(h *DomainHandler, svc *MockDomainService) (any, error) {
			svc.EXPECT().Create(gomock.Any(), int64(3), int64(7), "links.example.com").
				Return(nil, assert.AnError)

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/domains",
				`{"hostname":"links.example.com"}`, orgOwnerClaims(), nil)

			return h.Create(ctx)
		}},
		{"list", func(h *DomainHandler, svc *MockDomainService) (any, error) {
			svc.EXPECT().List(gomock.Any(), int64(3)).Return([]models.Domain(nil), assert.AnError)

			ctx := newTestCtx(t, http.MethodGet, "/api/v1/org/domains", "", orgOwnerClaims(), nil)

			return h.List(ctx)
		}},
		{"verify", func(h *DomainHandler, svc *MockDomainService) (any, error) {
			svc.EXPECT().Verify(gomock.Any(), int64(3), int64(7), int64(21)).Return(nil, assert.AnError)

			ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/domains/21/verify", "",
				orgOwnerClaims(), map[string]string{"id": "21"})

			return h.Verify(ctx)
		}},
		{"set primary", func(h *DomainHandler, svc *MockDomainService) (any, error) {
			svc.EXPECT().SetPrimary(gomock.Any(), int64(3), int64(7), int64(21)).Return(nil, assert.AnError)

			ctx := newTestCtx(t, http.MethodPut, "/api/v1/org/domains/21/primary", "",
				orgOwnerClaims(), map[string]string{"id": "21"})

			return h.SetPrimary(ctx)
		}},
		{"delete", func(h *DomainHandler, svc *MockDomainService) (any, error) {
			svc.EXPECT().Delete(gomock.Any(), int64(3), int64(7), int64(21)).Return(assert.AnError)

			ctx := newTestCtx(t, http.MethodDelete, "/api/v1/org/domains/21", "",
				orgOwnerClaims(), map[string]string{"id": "21"})

			return h.Delete(ctx)
		}},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			h, svc := newDomainHandler(t)

			got, err := tc.call(h, svc)

			require.Error(t, err)
			assert.True(t, got == nil, "handler must return an untyped nil on error, got %#v", got)
		})
	}
}

func TestDomainHandler_List(t *testing.T) {
	h, svc := newDomainHandler(t)

	domains := []models.Domain{*testDomain(models.DomainStatusVerified)}
	svc.EXPECT().List(gomock.Any(), int64(3)).Return(domains, nil)

	ctx := newTestCtx(t, http.MethodGet, "/api/v1/org/domains", "", orgOwnerClaims(), nil)

	got, err := h.List(ctx)
	require.NoError(t, err)
	assert.Len(t, got.([]models.Domain), 1)
}

func TestDomainHandler_Verify(t *testing.T) {
	t.Run("verified", func(t *testing.T) {
		h, svc := newDomainHandler(t)

		verified := testDomain(models.DomainStatusVerified)
		svc.EXPECT().Verify(gomock.Any(), int64(3), int64(7), int64(21)).Return(verified, nil)

		ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/domains/21/verify", "",
			orgOwnerClaims(), map[string]string{"id": "21"})

		got, err := h.Verify(ctx)
		require.NoError(t, err)
		assert.Equal(t, verified, got)
	})

	t.Run("TXT not proven surfaces the 409", func(t *testing.T) {
		h, svc := newDomainHandler(t)

		svc.EXPECT().Verify(gomock.Any(), int64(3), int64(7), int64(21)).
			Return(nil, apierrors.Conflict("the verification TXT record could not be found yet"))

		ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/domains/21/verify", "",
			orgOwnerClaims(), map[string]string{"id": "21"})

		_, err := h.Verify(ctx)
		require.Error(t, err)

		var apiErr apierrors.Error
		require.ErrorAs(t, err, &apiErr)
		assert.Equal(t, http.StatusConflict, apiErr.StatusCode())
	})

	t.Run("invalid id", func(t *testing.T) {
		h, _ := newDomainHandler(t)

		ctx := newTestCtx(t, http.MethodPost, "/api/v1/org/domains/x/verify", "",
			orgOwnerClaims(), map[string]string{"id": "x"})

		_, err := h.Verify(ctx)
		require.Error(t, err)
	})
}

func TestDomainHandler_SetPrimary(t *testing.T) {
	h, svc := newDomainHandler(t)

	primary := testDomain(models.DomainStatusVerified)
	primary.IsPrimary = true

	svc.EXPECT().SetPrimary(gomock.Any(), int64(3), int64(7), int64(21)).Return(primary, nil)

	ctx := newTestCtx(t, http.MethodPut, "/api/v1/org/domains/21/primary", "",
		orgOwnerClaims(), map[string]string{"id": "21"})

	got, err := h.SetPrimary(ctx)
	require.NoError(t, err)
	assert.True(t, got.(*models.Domain).IsPrimary)
}

func TestDomainHandler_Delete(t *testing.T) {
	h, svc := newDomainHandler(t)

	svc.EXPECT().Delete(gomock.Any(), int64(3), int64(7), int64(21)).Return(nil)

	ctx := newTestCtx(t, http.MethodDelete, "/api/v1/org/domains/21", "",
		orgOwnerClaims(), map[string]string{"id": "21"})

	got, err := h.Delete(ctx)
	require.NoError(t, err)
	assert.Equal(t, map[string]bool{"deleted": true}, got)
}
