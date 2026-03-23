package tenant

import "time"

// Role constants for tenant membership.
const (
	RoleOwner  = "owner"
	RoleAdmin  = "admin"
	RoleMember = "member"
)

// Tenant represents a workspace in the platform.
type Tenant struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Slug         string            `json:"slug"`
	Plan         string            `json:"plan"`
	Mode         string            `json:"mode"` // "saas" or "selfhosted"
	K8sNamespace string            `json:"k8s_namespace"`
	Settings     map[string]any    `json:"settings"`
	CreatedAt    time.Time         `json:"created_at"`
	UpdatedAt    time.Time         `json:"updated_at"`
}

// User represents an authenticated user. GitHubID is nil for non-GitHub users
// (e.g., admin-token users in self-hosted mode).
type User struct {
	ID          string    `json:"id"`
	GitHubID    *int64    `json:"github_id"`
	GitHubLogin string    `json:"github_login"`
	GitHubEmail string    `json:"github_email"`
	AvatarURL   string    `json:"avatar_url"`
	Name        string    `json:"name"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// TenantMember links a user to a tenant with a specific role.
type TenantMember struct {
	ID        string    `json:"id"`
	TenantID  string    `json:"tenant_id"`
	UserID    string    `json:"user_id"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`

	// Embedded relations populated by JOIN queries.
	Tenant *Tenant `json:"tenant,omitempty"`
	User   *User   `json:"user,omitempty"`
}
