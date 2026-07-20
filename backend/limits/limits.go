// Package limits defines the policy seam that lets a deployment bound
// resource creation and analytics visibility.
//
// A deployment wires a Policy into main.go; the default, Unlimited, allows
// everything, so a stock deployment behaves as if the seam did not exist.
//
// The Policy interface grows additively: it covers org creation, link
// creation, custom domains, membership, API keys and the analytics window,
// and future versions may add more checks. To stay compatible across such
// additions, every implementation MUST embed UnimplementedPolicy (enforced by
// the unexported mustEmbedUnimplementedPolicy method) and override only the
// checks it enforces. Checks it does not override fall through to
// UnimplementedPolicy, which always allows — so an implementation written
// against today's interface keeps compiling and keeps its behavior when new
// checks appear (the same forward-compatibility pattern gRPC uses for service
// servers).
//
// A check blocks an action by returning Deny("human-readable reason"); the
// API layer turns that into HTTP 403 with the machine-readable error code
// "LIMIT_REACHED" and passes the reason through to the user verbatim. Any
// other non-nil error from a check is treated as an internal policy failure,
// not a denial.
//
// Policy implementations that bound resources by current usage compose with
// the open-core usage.Reader (backend/usage): construct the implementation
// with a Reader and consult its counters inside each check. The core never
// couples the two — an implementation is free to decide however it likes.
//
// One structural guarantee is deliberately outside this seam: the resolution
// / redirect path takes no Policy dependency at all, so no policy — however
// misconfigured — can ever break a short link or stop click recording
// (FEATURES.md INV-7).
package limits

import (
	"time"

	"gofr.dev/pkg/gofr"
)

// Policy decides whether resource-creating actions are allowed in this
// deployment, and how much recorded analytics an org may view.
// Implementations must embed UnimplementedPolicy; see the package doc for the
// forward-compatibility contract.
type Policy interface {
	// CanCreateOrg reports whether userID may create another org.
	// nil allows; a *Denial (built with Deny) blocks the action with its
	// message; any other error is an internal policy failure.
	CanCreateOrg(ctx *gofr.Context, userID int64) error

	// CanCreateLink reports whether a new link may be created in the org.
	// userID is the creating user, or 0 when the link is created via a
	// developer API key. Consulted on creation only — never on edits
	// (alias, deep link, destination), which change an existing resource.
	CanCreateLink(ctx *gofr.Context, orgID, userID int64) error

	// CanAddDomain reports whether the org may register another custom
	// domain.
	CanAddDomain(ctx *gofr.Context, orgID int64) error

	// CanAddMember reports whether the org may take another member. It is
	// consulted when an invite is created and again at every membership
	// creation on the login path (invite acceptance, auto-join by email
	// domain); a denial there skips the join without failing the login.
	CanAddMember(ctx *gofr.Context, orgID int64) error

	// CanCreateAPIKey reports whether the org may create another developer
	// API key.
	CanCreateAPIKey(ctx *gofr.Context, orgID int64) error

	// AnalyticsWindow returns the org's analytics viewing bounds; the zero
	// Window means unbounded. An error is an internal policy failure (it
	// never denies). Recording is out of scope by design: clicks are always
	// recorded in full — the window only bounds what stats queries show.
	AnalyticsWindow(ctx *gofr.Context, orgID int64) (Window, error)

	// mustEmbedUnimplementedPolicy forces implementations to embed
	// UnimplementedPolicy so that checks added to this interface later do
	// not break them.
	mustEmbedUnimplementedPolicy()
}

// Window bounds what recorded analytics an org may view. The zero value is
// fully unbounded (the UnimplementedPolicy default).
type Window struct {
	// ViewableEvents caps VIEWING, not recording: when > 0 and the org's
	// current-calendar-month event (click) count exceeds it, stats endpoints
	// answer 403 LIMIT_REACHED — while clicks keep recording and redirects
	// keep working. 0 = unbounded.
	ViewableEvents int64

	// Retention bounds how far back stats queries may look: the from-date of
	// every stats query is clamped to now minus Retention. Older events stay
	// stored (recording is never bounded) — they are just not viewable while
	// this Window applies. 0 = unbounded.
	Retention time.Duration

	// Message is shown (verbatim, as the LIMIT_REACHED message) when
	// ViewableEvents gates a stats endpoint. Empty falls back to
	// DefaultWindowMessage, so implementations only set it to customize the
	// wording.
	Message string
}

// DefaultWindowMessage is the LIMIT_REACHED message used when a Window's
// ViewableEvents bound gates analytics viewing and the policy set no Message.
const DefaultWindowMessage = "This organization has exceeded the number of events viewable under the current policy. " +
	"Clicks are still being recorded and links keep working."

// UnimplementedPolicy is the forward-compatible base every Policy
// implementation must embed. It answers "allow" (nil, or the unbounded zero
// Window) for every check, so implementations override only the checks they
// enforce and inherit an allow for everything else — including checks added
// after they were written.
type UnimplementedPolicy struct{}

// CanCreateOrg allows by default.
func (UnimplementedPolicy) CanCreateOrg(*gofr.Context, int64) error { return nil }

// CanCreateLink allows by default.
func (UnimplementedPolicy) CanCreateLink(*gofr.Context, int64, int64) error { return nil }

// CanAddDomain allows by default.
func (UnimplementedPolicy) CanAddDomain(*gofr.Context, int64) error { return nil }

// CanAddMember allows by default.
func (UnimplementedPolicy) CanAddMember(*gofr.Context, int64) error { return nil }

// CanCreateAPIKey allows by default.
func (UnimplementedPolicy) CanCreateAPIKey(*gofr.Context, int64) error { return nil }

// AnalyticsWindow is unbounded by default.
func (UnimplementedPolicy) AnalyticsWindow(*gofr.Context, int64) (Window, error) {
	return Window{}, nil
}

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
