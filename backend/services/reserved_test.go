package services

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestReservedAliases_CategoryScoping(t *testing.T) {
	r := NewReservedAliases(nil)

	tests := []struct {
		word           string
		sharedReserved bool // on SHORT_DOMAIN (full list)
		customReserved bool // org with a VERIFIED custom domain (functional only)
	}{
		// Functional/infra: reserved in BOTH scopes (routing is path-based on
		// every host).
		{"api", true, true},
		{"metrics", true, true},
		{"assets", true, true},
		{"cdn", true, true},
		{"www", true, true},
		{"mail", true, true},
		{"webhook", true, true},
		{".well-known", true, true},

		// Auth-shaped: shared domain only.
		{"login", true, false},
		{"signin", true, false},
		{"signup", true, false},
		{"password", true, false},
		{"oauth", true, false},
		{"sso", true, false},
		{"admin", true, false},
		{"dashboard", true, false},

		// Legal/brand: shared domain only.
		{"pricing", true, false},
		{"terms", true, false},
		{"privacy", true, false},
		{"features", true, false},
		{"compare", true, false},
		{"self-host", true, false},
		{"docs", true, false},
		{"blog", true, false},
		{"status", true, false},
		{"abuse", true, false},
		{"security", true, false},
		{"support", true, false},
		{"help", true, false},
		{"contact", true, false},
		{"about", true, false},
		{"report", true, false},

		// Case-insensitive.
		{"Pricing", true, false},
		{"API", true, true},

		// Not reserved anywhere.
		{"my-campaign", false, false},
		{"abc1234", false, false},
	}

	for _, tc := range tests {
		t.Run(tc.word, func(t *testing.T) {
			assert.Equal(t, tc.sharedReserved, r.IsReserved(tc.word, false), "shared-domain scope")
			assert.Equal(t, tc.customReserved, r.IsReserved(tc.word, true), "custom-domain scope")
		})
	}
}

func TestReservedAliases_ConfigAdditionsApplyInBothScopes(t *testing.T) {
	r := NewReservedAliases([]string{" BrandWord ", "internal-tool", "", "  "})

	// Operator-mandated words are reserved everywhere, normalized.
	assert.True(t, r.IsReserved("brandword", false))
	assert.True(t, r.IsReserved("BRANDWORD", true))
	assert.True(t, r.IsReserved("internal-tool", false))
	assert.True(t, r.IsReserved("internal-tool", true))

	// Blank additions are dropped; built-ins unchanged.
	assert.False(t, r.IsReserved("", false))
	assert.True(t, r.IsReserved("pricing", false))
	assert.False(t, r.IsReserved("pricing", true))
}
