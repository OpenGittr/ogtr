// Package limits defines the policy seam that lets a deployment bound
// resource creation.
//
// A deployment wires a Policy into main.go; the default, Unlimited, allows
// everything, so a stock deployment behaves as if the seam did not exist.
//
// The Policy interface grows additively: today it covers org creation, and
// future versions may add checks for link creation, custom domains, seats, or
// analytics windows. To stay compatible across such additions, every
// implementation MUST embed UnimplementedPolicy (enforced by the unexported
// mustEmbedUnimplementedPolicy method) and override only the checks it
// enforces. Checks it does not override fall through to UnimplementedPolicy,
// which always allows — so an implementation written against today's
// interface keeps compiling and keeps its behavior when new checks appear
// (the same forward-compatibility pattern gRPC uses for service servers).
//
// A check blocks an action by returning Deny("human-readable reason"); the
// API layer turns that into HTTP 403 with the machine-readable error code
// "LIMIT_REACHED" and passes the reason through to the user verbatim. Any
// other non-nil error from a check is treated as an internal policy failure,
// not a denial.
package limits

import "gofr.dev/pkg/gofr"

// Policy decides whether resource-creating actions are allowed in this
// deployment. Implementations must embed UnimplementedPolicy; see the package
// doc for the forward-compatibility contract.
type Policy interface {
	// CanCreateOrg reports whether userID may create another org.
	// nil allows; a *Denial (built with Deny) blocks the action with its
	// message; any other error is an internal policy failure.
	CanCreateOrg(ctx *gofr.Context, userID int64) error

	// mustEmbedUnimplementedPolicy forces implementations to embed
	// UnimplementedPolicy so that checks added to this interface later do
	// not break them.
	mustEmbedUnimplementedPolicy()
}

// UnimplementedPolicy is the forward-compatible base every Policy
// implementation must embed. It answers "allow" (nil) for every check, so
// implementations override only the checks they enforce and inherit an allow
// for everything else — including checks added after they were written.
type UnimplementedPolicy struct{}

// CanCreateOrg allows by default.
func (UnimplementedPolicy) CanCreateOrg(*gofr.Context, int64) error { return nil }

func (UnimplementedPolicy) mustEmbedUnimplementedPolicy() {}

// Unlimited is the default Policy: every check allows. A deployment that
// wires nothing else gets this.
type Unlimited struct {
	UnimplementedPolicy
}

// Compile-time interface checks.
var (
	_ Policy = Unlimited{}
	_ Policy = UnimplementedPolicy{}
)

// Denial is the error a Policy check returns to block an action. Its message
// is shown to the user verbatim; the API surfaces it as HTTP 403 with the
// machine-readable code "LIMIT_REACHED".
type Denial struct {
	msg string
}

// Deny builds the Denial a Policy check returns to block an action, carrying
// the human-readable reason to show the user.
func Deny(msg string) *Denial { return &Denial{msg: msg} }

// Error implements the error interface.
func (d *Denial) Error() string { return d.msg }
