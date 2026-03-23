package migration

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// TenantMigration represents a migration record owned by a tenant.
type TenantMigration struct {
	ID          string          `json:"id"`
	TenantID    string          `json:"tenantId"`
	CRName      string          `json:"crName"`
	CRNamespace string          `json:"crNamespace"`
	CRKind      string          `json:"crKind"`
	DisplayName string          `json:"displayName"`
	Config      json.RawMessage `json:"config"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// MigrationStore provides tenant-scoped CRUD operations for migrations.
type MigrationStore struct {
	pool *pgxpool.Pool
}

// NewMigrationStore returns a new MigrationStore backed by the given pool.
func NewMigrationStore(pool *pgxpool.Pool) *MigrationStore {
	return &MigrationStore{pool: pool}
}

// Create inserts a new tenant migration record.
func (s *MigrationStore) Create(ctx context.Context, m *TenantMigration) error {
	query := `
		INSERT INTO tenant_migrations (
			id, tenant_id, cr_name, cr_namespace, cr_kind,
			display_name, config, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`

	now := time.Now().UTC()
	m.CreatedAt = now
	m.UpdatedAt = now

	_, err := s.pool.Exec(ctx, query,
		m.ID, m.TenantID, m.CRName, m.CRNamespace, m.CRKind,
		m.DisplayName, m.Config, m.Status, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert tenant migration: %w", err)
	}
	return nil
}

// GetByID retrieves a single migration scoped to the given tenant.
func (s *MigrationStore) GetByID(ctx context.Context, tenantID, id string) (*TenantMigration, error) {
	query := `
		SELECT id, tenant_id, cr_name, cr_namespace, cr_kind,
		       display_name, config, status, created_at, updated_at
		FROM tenant_migrations
		WHERE tenant_id = $1 AND id = $2`

	var m TenantMigration
	err := s.pool.QueryRow(ctx, query, tenantID, id).Scan(
		&m.ID, &m.TenantID, &m.CRName, &m.CRNamespace, &m.CRKind,
		&m.DisplayName, &m.Config, &m.Status, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, fmt.Errorf("migration %s not found for tenant %s", id, tenantID)
		}
		return nil, fmt.Errorf("get tenant migration: %w", err)
	}
	return &m, nil
}

// List returns paginated migrations for a tenant along with the total count.
func (s *MigrationStore) List(ctx context.Context, tenantID string, limit, offset int) ([]TenantMigration, int, error) {
	countQuery := `SELECT COUNT(*) FROM tenant_migrations WHERE tenant_id = $1`
	var total int
	if err := s.pool.QueryRow(ctx, countQuery, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tenant migrations: %w", err)
	}

	if total == 0 {
		return []TenantMigration{}, 0, nil
	}

	query := `
		SELECT id, tenant_id, cr_name, cr_namespace, cr_kind,
		       display_name, config, status, created_at, updated_at
		FROM tenant_migrations
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`

	rows, err := s.pool.Query(ctx, query, tenantID, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list tenant migrations: %w", err)
	}
	defer rows.Close()

	var items []TenantMigration
	for rows.Next() {
		var m TenantMigration
		if err := rows.Scan(
			&m.ID, &m.TenantID, &m.CRName, &m.CRNamespace, &m.CRKind,
			&m.DisplayName, &m.Config, &m.Status, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan tenant migration row: %w", err)
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate tenant migration rows: %w", err)
	}

	return items, total, nil
}

// UpdateStatus sets the status of a migration scoped to the given tenant.
func (s *MigrationStore) UpdateStatus(ctx context.Context, tenantID, id, status string) error {
	query := `
		UPDATE tenant_migrations
		SET status = $1, updated_at = $2
		WHERE tenant_id = $3 AND id = $4`

	tag, err := s.pool.Exec(ctx, query, status, time.Now().UTC(), tenantID, id)
	if err != nil {
		return fmt.Errorf("update tenant migration status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("migration %s not found for tenant %s", id, tenantID)
	}
	return nil
}

// Delete removes a migration record scoped to the given tenant.
func (s *MigrationStore) Delete(ctx context.Context, tenantID, id string) error {
	query := `DELETE FROM tenant_migrations WHERE tenant_id = $1 AND id = $2`

	tag, err := s.pool.Exec(ctx, query, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete tenant migration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("migration %s not found for tenant %s", id, tenantID)
	}
	return nil
}
