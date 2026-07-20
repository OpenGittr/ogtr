package services

import (
	"errors"

	"github.com/opengittr/ogtr/backend/apierrors"
	"github.com/opengittr/ogtr/backend/limits"
)

// limitError maps a limits.Policy check error onto the API contract
// (ARCHITECTURE.md §8 "Extension seam: LimitsPolicy"): a *limits.Denial
// becomes 403 with code LIMIT_REACHED and the policy's message passed through
// verbatim; any other error is an internal policy failure and is returned
// unchanged (500). Callers pass a non-nil error.
func limitError(err error) error {
	var denial *limits.Denial
	if errors.As(err, &denial) {
		return apierrors.LimitReached(denial.Error())
	}

	return err
}
