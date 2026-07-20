package auth

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProviders(t *testing.T) {
	tests := []struct {
		desc    string
		raw     string
		want    []string
		wantErr string
	}{
		{desc: "unset defaults to google", raw: "", want: []string{"google"}},
		{desc: "blank defaults to google", raw: "   ", want: []string{"google"}},
		{desc: "google only", raw: "google", want: []string{"google"}},
		{desc: "dev only", raw: "dev", want: []string{"dev"}},
		{desc: "google and dev", raw: "google,dev", want: []string{"google", "dev"}},
		{desc: "order preserved", raw: "dev,google", want: []string{"dev", "google"}},
		{desc: "spaces and case are tolerated", raw: " Google , DEV ", want: []string{"google", "dev"}},
		{desc: "duplicates collapse", raw: "google,google,dev", want: []string{"google", "dev"}},
		{desc: "stray commas are ignored", raw: "google,,dev,", want: []string{"google", "dev"}},
		{
			// The startup-refusal path: main() Fatals on this error.
			desc: "unknown provider is a hard error", raw: "google,okta",
			wantErr: `unknown auth provider "okta"`,
		},
		{desc: "typo is a hard error", raw: "goggle", wantErr: `unknown auth provider "goggle"`},
		{desc: "only commas is a hard error", raw: ",,", wantErr: "contains no providers"},
	}

	for _, tc := range tests {
		t.Run(tc.desc, func(t *testing.T) {
			got, err := ParseProviders(tc.raw)

			if tc.wantErr != "" {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.wantErr)
				assert.Nil(t, got)

				return
			}

			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}
