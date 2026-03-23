package tenant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantStore provides data access for tenants, users, and memberships.
type TenantStore struct {
	pool *pgxpool.Pool
}

// NewTenantStore returns a new TenantStore backed by the given connection pool.
func NewTenantStore(pool *pgxpool.Pool) *TenantStore {
	return &TenantStore{pool: pool}
}

// CreateTenant inserts a new tenant row.
func (s *TenantStore) CreateTenant(ctx context.Context, t *Tenant) error {
	settingsJSON, err := json.Marshal(t.Settings)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	now := time.Now().UTC()
	t.CreatedAt = now
	t.UpdatedAt = now

	_, err = s.pool.Exec(ctx, `
		INSERT INTO tenants (id, name, slug, plan, mode, k8s_namespace, settings, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		t.ID, t.Name, t.Slug, t.Plan, t.Mode, t.K8sNamespace, settingsJSON, t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert tenant: %w", err)
	}
	return nil
}

// GetTenantByID retrieves a tenant by its primary key.
func (s *TenantStore) GetTenantByID(ctx context.Context, id string) (*Tenant, error) {
	return s.getTenant(ctx, `SELECT id, name, slug, plan, mode, k8s_namespace, settings, created_at, updated_at FROM tenants WHERE id = $1`, id)
}

// GetTenantBySlug retrieves a tenant by its unique slug.
func (s *TenantStore) GetTenantBySlug(ctx context.Context, slug string) (*Tenant, error) {
	return s.getTenant(ctx, `SELECT id, name, slug, plan, mode, k8s_namespace, settings, created_at, updated_at FROM tenants WHERE slug = $1`, slug)
}

func (s *TenantStore) getTenant(ctx context.Context, query string, arg any) (*Tenant, error) {
	var t Tenant
	var settingsJSON []byte

	err := s.pool.QueryRow(ctx, query, arg).Scan(
		&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Mode, &t.K8sNamespace,
		&settingsJSON, &t.CreatedAt, &t.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("tenant not found: %w", err)
		}
		return nil, fmt.Errorf("query tenant: %w", err)
	}

	if len(settingsJSON) > 0 {
		if err := json.Unmarshal(settingsJSON, &t.Settings); err != nil {
			return nil, fmt.Errorf("unmarshal settings: %w", err)
		}
	}
	return &t, nil
}

// UpdateTenant updates mutable tenant fields.
func (s *TenantStore) UpdateTenant(ctx context.Context, t *Tenant) error {
	settingsJSON, err := json.Marshal(t.Settings)
	if err != nil {
		return fmt.Errorf("marshal settings: %w", err)
	}

	t.UpdatedAt = time.Now().UTC()

	tag, err := s.pool.Exec(ctx, `
		UPDATE tenants
		SET name = $1, slug = $2, plan = $3, mode = $4, k8s_namespace = $5, settings = $6, updated_at = $7
		WHERE id = $8`,
		t.Name, t.Slug, t.Plan, t.Mode, t.K8sNamespace, settingsJSON, t.UpdatedAt, t.ID,
	)
	if err != nil {
		return fmt.Errorf("update tenant: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("tenant %s not found", t.ID)
	}
	return nil
}

// UpsertUser inserts a user or updates on github_id conflict, returning the resulting row.
func (s *TenantStore) UpsertUser(ctx context.Context, u *User) (*User, error) {
	now := time.Now().UTC()

	var result User
	err := s.pool.QueryRow(ctx, `
		INSERT INTO users (id, github_id, github_login, github_email, avatar_url, name, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $7)
		ON CONFLICT (github_id) DO UPDATE
		SET github_login = EXCLUDED.github_login,
		    github_email = EXCLUDED.github_email,
		    avatar_url   = EXCLUDED.avatar_url,
		    name         = EXCLUDED.name,
		    updated_at   = $7
		RETURNING id, github_id, github_login, github_email, avatar_url, name, created_at, updated_at`,
		u.ID, u.GitHubID, u.GitHubLogin, u.GitHubEmail, u.AvatarURL, u.Name, now,
	).Scan(
		&result.ID, &result.GitHubID, &result.GitHubLogin, &result.GitHubEmail,
		&result.AvatarURL, &result.Name, &result.CreatedAt, &result.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("upsert user: %w", err)
	}
	return &result, nil
}

// GetUserByGitHubID retrieves a user by their GitHub ID.
func (s *TenantStore) GetUserByGitHubID(ctx context.Context, githubID int64) (*User, error) {
	return s.getUser(ctx, `SELECT id, github_id, github_login, github_email, avatar_url, name, created_at, updated_at FROM users WHERE github_id = $1`, githubID)
}

// GetUserByID retrieves a user by their primary key.
func (s *TenantStore) GetUserByID(ctx context.Context, id string) (*User, error) {
	return s.getUser(ctx, `SELECT id, github_id, github_login, github_email, avatar_url, name, created_at, updated_at FROM users WHERE id = $1`, id)
}

func (s *TenantStore) getUser(ctx context.Context, query string, arg any) (*User, error) {
	var u User
	err := s.pool.QueryRow(ctx, query, arg).Scan(
		&u.ID, &u.GitHubID, &u.GitHubLogin, &u.GitHubEmail,
		&u.AvatarURL, &u.Name, &u.CreatedAt, &u.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("user not found: %w", err)
		}
		return nil, fmt.Errorf("query user: %w", err)
	}
	return &u, nil
}

// AddMember creates a tenant membership. If the user is already a member, this is a no-op.
func (s *TenantStore) AddMember(ctx context.Context, tenantID, userID, role string) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO tenant_members (id, tenant_id, user_id, role, created_at)
		VALUES (gen_random_uuid(), $1, $2, $3, $4)
		ON CONFLICT (tenant_id, user_id) DO NOTHING`,
		tenantID, userID, role, time.Now().UTC(),
	)
	if err != nil {
		return fmt.Errorf("add member: %w", err)
	}
	return nil
}

// GetMembership returns all tenants a user belongs to, with the Tenant embedded.
func (s *TenantStore) GetMembership(ctx context.Context, userID string) ([]TenantMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			tm.id, tm.tenant_id, tm.user_id, tm.role, tm.created_at,
			t.id, t.name, t.slug, t.plan, t.mode, t.k8s_namespace, t.settings, t.created_at, t.updated_at
		FROM tenant_members tm
		JOIN tenants t ON t.id = tm.tenant_id
		WHERE tm.user_id = $1
		ORDER BY tm.created_at`, userID)
	if err != nil {
		return nil, fmt.Errorf("query membership: %w", err)
	}
	defer rows.Close()

	var members []TenantMember
	for rows.Next() {
		var m TenantMember
		var t Tenant
		var settingsJSON []byte

		if err := rows.Scan(
			&m.ID, &m.TenantID, &m.UserID, &m.Role, &m.CreatedAt,
			&t.ID, &t.Name, &t.Slug, &t.Plan, &t.Mode, &t.K8sNamespace, &settingsJSON, &t.CreatedAt, &t.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan membership row: %w", err)
		}

		if len(settingsJSON) > 0 {
			if err := json.Unmarshal(settingsJSON, &t.Settings); err != nil {
				return nil, fmt.Errorf("unmarshal tenant settings: %w", err)
			}
		}
		m.Tenant = &t
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate membership rows: %w", err)
	}
	return members, nil
}

// GetTenantMembers returns all members of a tenant, with the User embedded.
func (s *TenantStore) GetTenantMembers(ctx context.Context, tenantID string) ([]TenantMember, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT
			tm.id, tm.tenant_id, tm.user_id, tm.role, tm.created_at,
			u.id, u.github_id, u.github_login, u.github_email, u.avatar_url, u.name, u.created_at, u.updated_at
		FROM tenant_members tm
		JOIN users u ON u.id = tm.user_id
		WHERE tm.tenant_id = $1
		ORDER BY tm.created_at`, tenantID)
	if err != nil {
		return nil, fmt.Errorf("query tenant members: %w", err)
	}
	defer rows.Close()

	var members []TenantMember
	for rows.Next() {
		var m TenantMember
		var u User

		if err := rows.Scan(
			&m.ID, &m.TenantID, &m.UserID, &m.Role, &m.CreatedAt,
			&u.ID, &u.GitHubID, &u.GitHubLogin, &u.GitHubEmail, &u.AvatarURL, &u.Name, &u.CreatedAt, &u.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan tenant member row: %w", err)
		}

		m.User = &u
		members = append(members, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate tenant member rows: %w", err)
	}
	return members, nil
}

// IsOwner checks whether the given user is an owner of the specified tenant.
func (s *TenantStore) IsOwner(ctx context.Context, tenantID, userID string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS(
			SELECT 1 FROM tenant_members
			WHERE tenant_id = $1 AND user_id = $2 AND role = $3
		)`, tenantID, userID, RoleOwner).Scan(&exists)
	if err != nil {
		return false, fmt.Errorf("check ownership: %w", err)
	}
	return exists, nil
}
