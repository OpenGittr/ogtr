package handlers

import (
	"gofr.dev/pkg/gofr"
	gofrHTTP "gofr.dev/pkg/gofr/http"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/services"
)

// citySuggestionLimit caps the /api/v1/cities autocomplete response.
const citySuggestionLimit = 20

// RuleHandler serves target-rule CRUD (/api/v1/links/{id}/rules,
// /api/v1/rules/{ruleId}) and the rule-builder city autocomplete
// (/api/v1/cities). The org is always the token's org_id claim.
type RuleHandler struct {
	rules  RuleService
	cities CityIndex
}

// NewRuleHandler wires a RuleHandler.
func NewRuleHandler(rules RuleService, cities CityIndex) *RuleHandler {
	return &RuleHandler{rules: rules, cities: cities}
}

// Create handles POST /api/v1/links/{id}/rules — appends an array of rules
// in the submitted order (which is the evaluation order).
func (h *RuleHandler) Create(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	linkID, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	var inputs []services.RuleInput
	if err := ctx.Bind(&inputs); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	// Return an untyped nil on error: a typed-nil []models.Rule inside the
	// any return makes gofr treat the response as partial content (206)
	// instead of using the error's status code.
	created, err := h.rules.Create(ctx, claims.OrgID, claims.UserID, linkID, inputs)
	if err != nil {
		return nil, err
	}

	return created, nil
}

// List handles GET /api/v1/links/{id}/rules.
func (h *RuleHandler) List(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	linkID, err := pathID(ctx, "id")
	if err != nil {
		return nil, err
	}

	// Untyped nil on error — see Create.
	rules, err := h.rules.List(ctx, claims.OrgID, claims.UserID, linkID)
	if err != nil {
		return nil, err
	}

	return rules, nil
}

// Update handles PUT /api/v1/rules/{ruleId}.
func (h *RuleHandler) Update(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	ruleID, err := pathID(ctx, "ruleId")
	if err != nil {
		return nil, err
	}

	var in services.RuleInput
	if err := ctx.Bind(&in); err != nil {
		return nil, gofrHTTP.ErrorInvalidParam{Params: []string{"body"}}
	}

	return h.rules.Update(ctx, claims.OrgID, claims.UserID, ruleID, in)
}

// Delete handles DELETE /api/v1/rules/{ruleId}.
func (h *RuleHandler) Delete(ctx *gofr.Context) (any, error) {
	claims, err := orgClaims(ctx)
	if err != nil {
		return nil, err
	}

	ruleID, err := pathID(ctx, "ruleId")
	if err != nil {
		return nil, err
	}

	if err := h.rules.Delete(ctx, claims.OrgID, claims.UserID, ruleID); err != nil {
		return nil, err
	}

	return map[string]bool{"deleted": true}, nil
}

// Cities handles GET /api/v1/cities?q= — city-name prefix autocomplete for
// the rule builder. 501 when the deployment has no city dataset
// (GEOIP_CITIES_CSV unset — ARCHITECTURE.md §6).
func (h *RuleHandler) Cities(ctx *gofr.Context) (any, error) {
	if _, err := orgClaims(ctx); err != nil {
		return nil, err
	}

	if !h.cities.Enabled() {
		return nil, apierrors.NotImplemented("city autocomplete is not configured on this deployment")
	}

	q := ctx.Param("q")
	if q == "" {
		return nil, gofrHTTP.ErrorMissingParam{Params: []string{"q"}}
	}

	return h.cities.Search(q, citySuggestionLimit), nil
}
