package handlers

import (
	"strconv"
	"strings"

	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"
)

// StatsHandler serves the analytics endpoints (ARCHITECTURE.md §4
// "Analytics"). The org is always the org_id claim of the access token.
type StatsHandler struct {
	stats StatsService
}

// NewStatsHandler wires a StatsHandler.
func NewStatsHandler(stats StatsService) *StatsHandler {
	return &StatsHandler{stats: stats}
}

// LinkReport handles GET /api/v1/links/{id}/stats?from=&to=&deeplink= — the
// full per-link report (FEATURES.md §5.1). Dates are YYYY-MM-DD, both
// inclusive, defaulting to the last month; from > to is 400.
func (h *StatsHandler) LinkReport(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	id, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	deeplink := false

	if raw := ctx.Param("deeplink"); raw != "" {
		deeplink, err = strconv.ParseBool(raw)
		if err != nil {
			return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"deeplink"}}
		}
	}

	// Untyped nil on error — a typed nil pointer would make gofr respond 206.
	report, err := h.stats.LinkReport(ctx, claims.OrgID, claims.UserID, id,
		ctx.Param("from"), ctx.Param("to"), deeplink)
	if err != nil {
		return nil, err
	}

	return report, nil
}

// UniqueClicks handles GET /api/v1/stats/unique-clicks?link_ids=1,2,3 —
// distinct campaign tags over the given links' clicks (FEATURES.md §5.2).
// An empty or malformed link_ids is 400.
func (h *StatsHandler) UniqueClicks(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	ids, err := parseLinkIDs(ctx.Param("link_ids"))
	if err != nil {
		return nil, err
	}

	result, err := h.stats.UniqueClicks(ctx, claims.OrgID, ids)
	if err != nil {
		return nil, err
	}

	return result, nil
}

// Tags handles GET /api/v1/stats/tags — every distinct campaign tag in the
// org's click data (FEATURES.md §5.3).
func (h *StatsHandler) Tags(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — a typed nil slice would make gofr respond 206.
	tags, err := h.stats.Tags(ctx, claims.OrgID)
	if err != nil {
		return nil, err
	}

	return tags, nil
}

// UTM handles GET /api/v1/stats/utm — the three UTM analyses (FEATURES.md
// §6.3) over the viewer-visible links of the org.
func (h *StatsHandler) UTM(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	analysis, err := h.stats.UTMAnalysis(ctx, claims.OrgID, claims.UserID)
	if err != nil {
		return nil, err
	}

	return analysis, nil
}

// parseLinkIDs parses a comma-separated id list; empty or non-positive
// entries are 400.
func parseLinkIDs(raw string) ([]int64, error) {
	invalid := gofrHTTP.ErrorInvalidParam{Params: []string{"link_ids"}}

	if strings.TrimSpace(raw) == "" {
		return nil, invalid
	}

	parts := strings.Split(raw, ",")
	ids := make([]int64, 0, len(parts))

	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 {
			return nil, invalid
		}

		ids = append(ids, id)
	}

	return ids, nil
}
