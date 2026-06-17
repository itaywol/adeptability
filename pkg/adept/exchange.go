package adept

// Exchange types model the team "expertise billboard": a self-hosted board
// where a developer (or their agent) posts a request for a teammate's
// expertise, and teammates stack responses on it. These are pure data
// structures — all behavior (storage, HTTP, auth) lives in internal/exchange.

// Exchange item statuses. New items start AttentionRequired; the first
// response auto-flips to InProgress; only the author may Close/reopen.
const (
	ExchangeStatusAttention  = "attention-required"
	ExchangeStatusInProgress = "in-progress"
	ExchangeStatusClosed     = "closed"
	ExchangeDefaultStatus    = ExchangeStatusAttention
)

// Exchange user roles. The server-raiser is the owner (may rotate the
// bootstrap token); everyone who registers afterwards is a member.
const (
	ExchangeRoleOwner  = "owner"
	ExchangeRoleMember = "member"
)

// ExchangeUser is a registered participant. Identity is the self-declared
// Handle; TokenHash is sha256(token) — the raw token is never stored.
type ExchangeUser struct {
	Handle    string `json:"handle"`
	TokenHash string `json:"tokenHash"`
	Role      string `json:"role"`
}

// ExchangeComment is one response stacked on an item.
type ExchangeComment struct {
	Author    string `json:"author"`
	Body      string `json:"body"`
	CreatedAt string `json:"createdAt"` // RFC3339
}

// ExchangeItem is a single expertise request on the billboard.
type ExchangeItem struct {
	ID        int               `json:"id"`
	Author    string            `json:"author"`
	Title     string            `json:"title"`
	Body      string            `json:"body"`
	Assignees []string          `json:"assignees,omitempty"`
	Tags      []string          `json:"tags,omitempty"`
	Status    string            `json:"status"`
	Comments  []ExchangeComment `json:"comments,omitempty"`
	CreatedAt string            `json:"createdAt"` // RFC3339
	UpdatedAt string            `json:"updatedAt"` // RFC3339
}

// ValidExchangeStatus reports whether s is a settable item status.
func ValidExchangeStatus(s string) bool {
	switch s {
	case ExchangeStatusAttention, ExchangeStatusInProgress, ExchangeStatusClosed:
		return true
	default:
		return false
	}
}
