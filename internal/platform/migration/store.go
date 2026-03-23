package migration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Migration represents a migration record.
type Migration struct {
	ID          string          `json:"id"`
	CRName      string          `json:"crName"`
	CRNamespace string          `json:"crNamespace"`
	CRKind      string          `json:"crKind"`
	DisplayName string          `json:"displayName"`
	Config      json.RawMessage `json:"config"`
	Status      string          `json:"status"`
	CreatedAt   time.Time       `json:"createdAt"`
	UpdatedAt   time.Time       `json:"updatedAt"`
}

// MigrationStore provides CRUD operations for migrations.
type MigrationStore struct {
	pool *pgxpool.Pool
}

// NewMigrationStore returns a new MigrationStore backed by the given pool.
func NewMigrationStore(pool *pgxpool.Pool) *MigrationStore {
	return &MigrationStore{pool: pool}
}

// Create inserts a new migration record.
func (s *MigrationStore) Create(ctx context.Context, m *Migration) error {
	query := `
		INSERT INTO migrations (
			id, cr_name, cr_namespace, cr_kind,
			display_name, config, status, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`

	now := time.Now().UTC()
	m.CreatedAt = now
	m.UpdatedAt = now

	_, err := s.pool.Exec(ctx, query,
		m.ID, m.CRName, m.CRNamespace, m.CRKind,
		m.DisplayName, m.Config, m.Status, m.CreatedAt, m.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("insert migration: %w", err)
	}
	return nil
}

// GetByID retrieves a single migration by ID.
func (s *MigrationStore) GetByID(ctx context.Context, id string) (*Migration, error) {
	query := `
		SELECT id, cr_name, cr_namespace, cr_kind,
		       display_name, config, status, created_at, updated_at
		FROM migrations
		WHERE id = $1`

	var m Migration
	err := s.pool.QueryRow(ctx, query, id).Scan(
		&m.ID, &m.CRName, &m.CRNamespace, &m.CRKind,
		&m.DisplayName, &m.Config, &m.Status, &m.CreatedAt, &m.UpdatedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, fmt.Errorf("migration %s not found", id)
		}
		return nil, fmt.Errorf("get migration: %w", err)
	}
	return &m, nil
}

// List returns paginated migrations along with the total count.
func (s *MigrationStore) List(ctx context.Context, limit, offset int) ([]Migration, int, error) {
	countQuery := `SELECT COUNT(*) FROM migrations`
	var total int
	if err := s.pool.QueryRow(ctx, countQuery).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count migrations: %w", err)
	}

	if total == 0 {
		return []Migration{}, 0, nil
	}

	query := `
		SELECT id, cr_name, cr_namespace, cr_kind,
		       display_name, config, status, created_at, updated_at
		FROM migrations
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`

	rows, err := s.pool.Query(ctx, query, limit, offset)
	if err != nil {
		return nil, 0, fmt.Errorf("list migrations: %w", err)
	}
	defer rows.Close()

	var items []Migration
	for rows.Next() {
		var m Migration
		if err := rows.Scan(
			&m.ID, &m.CRName, &m.CRNamespace, &m.CRKind,
			&m.DisplayName, &m.Config, &m.Status, &m.CreatedAt, &m.UpdatedAt,
		); err != nil {
			return nil, 0, fmt.Errorf("scan migration row: %w", err)
		}
		items = append(items, m)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate migration rows: %w", err)
	}

	return items, total, nil
}

// UpdateStatus sets the status of a migration.
func (s *MigrationStore) UpdateStatus(ctx context.Context, id, status string) error {
	query := `
		UPDATE migrations
		SET status = $1, updated_at = $2
		WHERE id = $3`

	tag, err := s.pool.Exec(ctx, query, status, time.Now().UTC(), id)
	if err != nil {
		return fmt.Errorf("update migration status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("migration %s not found", id)
	}
	return nil
}

// Delete removes a migration record.
func (s *MigrationStore) Delete(ctx context.Context, id string) error {
	query := `DELETE FROM migrations WHERE id = $1`

	tag, err := s.pool.Exec(ctx, query, id)
	if err != nil {
		return fmt.Errorf("delete migration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("migration %s not found", id)
	}
	return nil
}
