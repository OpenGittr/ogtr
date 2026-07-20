package services

// INV-7 (FEATURES.md §11): redirects never break because of the limits seam.
// This is guaranteed structurally — the resolution path takes no
// limits.Policy (or usage metering) dependency at all, so no policy state,
// bug, or misconfiguration can reach it. These tests pin that structure: if
// someone ever threads a policy or usage reader into ResolveService (fields
// or constructor), they fail and force a conscious decision.

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// forbiddenResolveDeps are the package paths that must never appear in the
// resolution path's dependency surface.
var forbiddenResolveDeps = []string{
	"github.com/opengittr/ogtr/backend/limits",
	"github.com/opengittr/ogtr/backend/usage",
}

// assertTypeFreeOf walks a type one level deep (pointers/slices unwrapped)
// and asserts it does not come from a forbidden package.
func assertTypeFreeOf(t *testing.T, typ reflect.Type, where string) {
	t.Helper()

	for typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice || typ.Kind() == reflect.Array {
		typ = typ.Elem()
	}

	for _, forbidden := range forbiddenResolveDeps {
		assert.False(t, strings.HasPrefix(typ.PkgPath(), forbidden),
			"%s must not depend on %s (INV-7: resolution can never be policy-bound)", where, forbidden)
	}
}

func TestResolveService_TakesNoPolicyDependency_INV7(t *testing.T) {
	svcType := reflect.TypeOf(ResolveService{})

	for i := range svcType.NumField() {
		field := svcType.Field(i)
		assertTypeFreeOf(t, field.Type, "ResolveService field "+field.Name)
	}

	ctorType := reflect.TypeOf(NewResolveService)
	for i := range ctorType.NumIn() {
		assertTypeFreeOf(t, ctorType.In(i), "NewResolveService parameter")
	}
}
