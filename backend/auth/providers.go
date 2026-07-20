package auth

import (
	"fmt"
	"strings"
)

// Provider names accepted in the AUTH_PROVIDERS config (ARCHITECTURE.md §5).
const (
	// ProviderGoogle is Google sign-in (OIDC ID tokens).
	ProviderGoogle = "google"
	// ProviderDev is the zero-setup development provider. It trusts the
	// submitted email/name without any credential proof and must NEVER be
	// enabled in production.
	ProviderDev = "dev"
)

// ParseProviders parses the AUTH_PROVIDERS config value: a comma-separated
// list of provider names ("google", "dev"). An empty/blank value defaults to
// just google. Unknown names are a hard error — the server refuses to start
// rather than silently running with a misconfigured auth surface. The result
// preserves order and drops duplicates.
func ParseProviders(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{ProviderGoogle}, nil
	}

	var (
		providers []string
		seen      = map[string]bool{}
	)

	for _, part := range strings.Split(raw, ",") {
		name := strings.ToLower(strings.TrimSpace(part))
		if name == "" {
			continue
		}

		if name != ProviderGoogle && name != ProviderDev {
			return nil, fmt.Errorf("unknown auth provider %q in AUTH_PROVIDERS (valid values: %s, %s)",
				name, ProviderGoogle, ProviderDev)
		}

		if !seen[name] {
			seen[name] = true

			providers = append(providers, name)
		}
	}

	if len(providers) == 0 {
		return nil, fmt.Errorf("AUTH_PROVIDERS %q contains no providers (valid values: %s, %s)",
			raw, ProviderGoogle, ProviderDev)
	}

	return providers, nil
}
