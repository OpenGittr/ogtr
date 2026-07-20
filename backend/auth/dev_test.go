package auth

import (
	"context"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gofr.dev/pkg/gofr"
	"gofr.dev/pkg/gofr/container"

	"github.com/opengittr/ogtr/backend/apierrors"
)

func devTestCtx(t *testing.T) *gofr.Context {
	t.Helper()

	mockContainer, _ := container.NewMockContainer(t)

	return &gofr.Context{Context: context.Background(), Container: mockContainer}
}

func TestDevProvider_Verify(t *testing.T) {
	tests := []struct {
		desc       string
		credential string
		want       Identity
		wantErr    string
	}{
		{
			desc:       "valid email and name",
			credential: EncodeDevCredential("eval@example.com", "Eval User"),
			want:       Identity{Email: "eval@example.com", Name: "Eval User"},
		},
		{
			desc:       "surrounding whitespace is trimmed",
			credential: EncodeDevCredential("  eval@example.com ", "  Eval User "),
			want:       Identity{Email: "eval@example.com", Name: "Eval User"},
		},
		{
			desc:       "empty name",
			credential: EncodeDevCredential("eval@example.com", ""),
			wantErr:    "name must not be empty",
		},
		{
			desc:       "whitespace-only name",
			credential: EncodeDevCredential("eval@example.com", "   "),
			wantErr:    "name must not be empty",
		},
		{
			desc:       "not an email",
			credential: EncodeDevCredential("not-an-email", "Eval User"),
			wantErr:    "email is not a valid email address",
		},
		{
			desc:       "empty email",
			credential: EncodeDevCredential("", "Eval User"),
			wantErr:    "email is not a valid email address",
		},
		{
			desc:       "display-name form is rejected",
			credential: EncodeDevCredential("Eval <eval@example.com>", "Eval User"),
			wantErr:    "email is not a valid email address",
		},
		{
			desc:       "malformed credential JSON",
			credential: `{"email":`,
			wantErr:    "invalid dev credential",
		},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := NewDevProvider().Verify(devTestCtx(t), tc.credential)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Equal(t, Identity{}, got)

				// Every dev validation failure is a 422 (semantically
				// invalid input), never a silent 500.
				var apiErr apierrors.Error

				require.ErrorAs(t, err, &apiErr)
				assert.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode())
				assert.Contains(t, apiErr.Error(), tc.wantErr)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
