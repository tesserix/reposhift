package services

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// DatabaseService handles PostgreSQL operations for migration tracking
type DatabaseService struct {
	pool *pgxpool.Pool
}

// MigrationRecord represents a migration tracking record
type MigrationRecord struct {
	ID                 string
	MigrationName      string
	MigrationNamespace string
	SourceOrganization string
	SourceProject      string
	SourceTeam         string
	TargetOwner        string
	TargetRepository   string
	Status             string
	TotalItems         int
	MigratedItems      int
	FailedItems        int
	SkippedItems       int
	StartedAt          *time.Time
	CompletedAt        *time.Time
	LastUpdatedAt      time.Time
	ErrorMessage       string
}

// WorkItemMigrationRecord represents an individual work item migration
type WorkItemMigrationRecord struct {
	ID                  string
	MigrationID         string
	AdoWorkItemID       int
	AdoWorkItemType     string
	AdoWorkItemTitle    string
	AdoWorkItemState    string
	GithubIssueNumber   *int
	GithubIssueURL      *string
	GithubIssueNodeID   *string
	GithubProjectItemID *string
	AddedToProjectAt    *time.Time
	Status              string
	MigratedAt          *time.Time
	ErrorMessage        *string
	RetryCount          int
	LastRetryAt         *time.Time
}

// GitHubProjectRecord represents a GitHub Project tracking record
type GitHubProjectRecord struct {
	ID                  string
	ProjectCRName       string
	ProjectCRNamespace  string
	GithubProjectID     string
	GithubProjectNumber int
	GithubProjectURL    string
	Owner               string
	ProjectName         string
	Template            string
	Public              bool
	Repository          string
	Status              string
	CreatedAt           time.Time
	LastUpdatedAt       time.Time
	ErrorMessage        string
}

// NewDatabaseService creates a new database service
func NewDatabaseService(ctx context.Context) (*DatabaseService, error) {
	// Check if PostgreSQL is enabled
	if os.Getenv("POSTGRES_ENABLED") != "true" {
		return nil, fmt.Errorf("PostgreSQL is not enabled")
	}

	host := os.Getenv("POSTGRES_HOST")
	port := os.Getenv("POSTGRES_PORT")
	user := os.Getenv("POSTGRES_USER")
	password := os.Getenv("POSTGRES_PASSWORD")
	database := os.Getenv("POSTGRES_DATABASE")
	sslmode := os.Getenv("POSTGRES_SSLMODE")

	fmt.Printf("Connecting to PostgreSQL database...\n")
	fmt.Printf("   Host: %s:%s\n", host, port)
	fmt.Printf("   Database: %s\n", database)
	fmt.Printf("   User: %s\n", user)
	fmt.Printf("   SSL Mode: %s\n", sslmode)

	// Build connection string
	connString := fmt.Sprintf(
		"host=%s port=%s user=%s dbname=%s sslmode=%s",
		host, port, user, database, sslmode,
	)
	if password != "" {
		connString += fmt.Sprintf(" password=%s", password)
	}

	// Create connection pool
	config, err := pgxpool.ParseConfig(connString)
	if err != nil {
		fmt.Printf("❌ Failed to parse database configuration: %v\n", err)
		return nil, fmt.Errorf("failed to parse database config: %w", err)
	}

	// Configure pool
	config.MaxConns = 10
	config.MinConns = 2
	config.MaxConnLifetime = time.Hour
	config.MaxConnIdleTime = 30 * time.Minute

	// Create pool
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		fmt.Printf("❌ Failed to create connection pool: %v\n", err)
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}

	// Test connection
	fmt.Printf("🔄 Testing database connection...\n")
	if err := pool.Ping(ctx); err != nil {
		fmt.Printf("❌ Failed to connect to PostgreSQL: %v\n", err)
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	fmt.Printf("✅ Successfully connected to PostgreSQL database\n")
	fmt.Printf("   Connection pool: MaxConns=%d, MinConns=%d\n", config.MaxConns, config.MinConns)

	return &DatabaseService{pool: pool}, nil
}

// Close closes the database connection pool
func (db *DatabaseService) Close() {
	if db.pool != nil {
		db.pool.Close()
	}
}

// InitializeMigration creates or updates a migration record
func (db *DatabaseService) InitializeMigration(ctx context.Context, record *MigrationRecord) (string, error) {
	query := `
		INSERT INTO migrations (
			migration_name, migration_namespace, source_organization, source_project,
			source_team, target_owner, target_repository, status, total_items,
			started_at, last_updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, NOW())
		ON CONFLICT (migration_name, migration_namespace)
		DO UPDATE SET
			status = EXCLUDED.status,
			total_items = EXCLUDED.total_items,
			started_at = EXCLUDED.started_at,
			last_updated_at = NOW()
		RETURNING id
	`

	var id string
	err := db.pool.QueryRow(ctx, query,
		record.MigrationName,
		record.MigrationNamespace,
		record.SourceOrganization,
		record.SourceProject,
		record.SourceTeam,
		record.TargetOwner,
		record.TargetRepository,
		record.Status,
		record.TotalItems,
		time.Now(),
	).Scan(&id)

	if err != nil {
		return "", fmt.Errorf("failed to initialize migration: %w", err)
	}

	fmt.Printf("📊 Initialized migration tracking: %s (ID: %s)\n", record.MigrationName, id)
	return id, nil
}

// IsWorkItemMigrated checks if a work item has already been migrated
func (db *DatabaseService) IsWorkItemMigrated(ctx context.Context, migrationID string, adoWorkItemID int) (bool, *WorkItemMigrationRecord, error) {
	query := `
		SELECT id, migration_id, ado_work_item_id, github_issue_number,
		       github_issue_url, status, migrated_at
		FROM work_item_migrations
		WHERE migration_id = $1 AND ado_work_item_id = $2 AND status = 'success'
	`

	var record WorkItemMigrationRecord
	err := db.pool.QueryRow(ctx, query, migrationID, adoWorkItemID).Scan(
		&record.ID,
		&record.MigrationID,
		&record.AdoWorkItemID,
		&record.GithubIssueNumber,
		&record.GithubIssueURL,
		&record.Status,
		&record.MigratedAt,
	)

	if err == pgx.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("failed to check work item: %w", err)
	}

	return true, &record, nil
}

// BatchCheckMigrated checks multiple work items in a single query (OPTIMIZATION: Task 1.3)
// Returns a map of ADO work item ID -> migration record for all already-migrated items
func (db *DatabaseService) BatchCheckMigrated(ctx context.Context, migrationID string, adoWorkItemIDs []int) (map[int]*WorkItemMigrationRecord, error) {
	if len(adoWorkItemIDs) == 0 {
		return make(map[int]*WorkItemMigrationRecord), nil
	}

	query := `
		SELECT id, migration_id, ado_work_item_id, github_issue_number,
		       github_issue_url, status, migrated_at
		FROM work_item_migrations
		WHERE migration_id = $1 AND ado_work_item_id = ANY($2) AND status = 'success'
	`

	rows, err := db.pool.Query(ctx, query, migrationID, adoWorkItemIDs)
	if err != nil {
		return nil, fmt.Errorf("failed to batch check work items: %w", err)
	}
	defer rows.Close()

	migratedMap := make(map[int]*WorkItemMigrationRecord)
	for rows.Next() {
		var record WorkItemMigrationRecord
		err := rows.Scan(
			&record.ID,
			&record.MigrationID,
			&record.AdoWorkItemID,
			&record.GithubIssueNumber,
			&record.GithubIssueURL,
			&record.Status,
			&record.MigratedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan batch check result: %w", err)
		}
		migratedMap[record.AdoWorkItemID] = &record
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating batch check results: %w", err)
	}

	return migratedMap, nil
}

// RecordWorkItemMigration records a successful work item migration
func (db *DatabaseService) RecordWorkItemMigration(ctx context.Context, record *WorkItemMigrationRecord) error {
	query := `
		INSERT INTO work_item_migrations (
			migration_id, ado_work_item_id, ado_work_item_type, ado_work_item_title,
			ado_work_item_state, github_issue_number, github_issue_url, status, migrated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (migration_id, ado_work_item_id)
		DO UPDATE SET
			github_issue_number = EXCLUDED.github_issue_number,
			github_issue_url = EXCLUDED.github_issue_url,
			status = EXCLUDED.status,
			migrated_at = EXCLUDED.migrated_at
	`

	_, err := db.pool.Exec(ctx, query,
		record.MigrationID,
		record.AdoWorkItemID,
		record.AdoWorkItemType,
		record.AdoWorkItemTitle,
		record.AdoWorkItemState,
		record.GithubIssueNumber,
		record.GithubIssueURL,
		record.Status,
		time.Now(),
	)

	if err != nil {
		return fmt.Errorf("failed to record work item migration: %w", err)
	}

	return nil
}

// BatchRecordMigrations records multiple work item migrations in a single transaction (OPTIMIZATION: Task 1.4)
// This is significantly faster than individual RecordWorkItemMigration calls
func (db *DatabaseService) BatchRecordMigrations(ctx context.Context, records []WorkItemMigrationRecord) error {
	if len(records) == 0 {
		return nil
	}

	// Use pgx batch for efficient bulk insert
	batch := &pgx.Batch{}

	query := `
		INSERT INTO work_item_migrations (
			migration_id, ado_work_item_id, ado_work_item_type, ado_work_item_title,
			ado_work_item_state, github_issue_number, github_issue_url, status, migrated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (migration_id, ado_work_item_id)
		DO UPDATE SET
			github_issue_number = EXCLUDED.github_issue_number,
			github_issue_url = EXCLUDED.github_issue_url,
			status = EXCLUDED.status,
			migrated_at = EXCLUDED.migrated_at
	`

	for _, record := range records {
		batch.Queue(query,
			record.MigrationID,
			record.AdoWorkItemID,
			record.AdoWorkItemType,
			record.AdoWorkItemTitle,
			record.AdoWorkItemState,
			record.GithubIssueNumber,
			record.GithubIssueURL,
			record.Status,
			time.Now(),
		)
	}

	br := db.pool.SendBatch(ctx, batch)
	defer br.Close()

	// Execute all batched queries
	for i := 0; i < len(records); i++ {
		_, err := br.Exec()
		if err != nil {
			return fmt.Errorf("failed to execute batch record at index %d: %w", i, err)
		}
	}

	return nil
}

// BatchRecordFailures records multiple failed work item migrations in a single transaction (OPTIMIZATION: Task 1.4)
func (db *DatabaseService) BatchRecordFailures(ctx context.Context, migrationID string, failures map[int]string) error {
	if len(failures) == 0 {
		return nil
	}

	batch := &pgx.Batch{}

	query := `
		INSERT INTO work_item_migrations (
			migration_id, ado_work_item_id, status, error_message, retry_count, last_retry_at
		) VALUES ($1, $2, 'failed', $3, 1, NOW())
		ON CONFLICT (migration_id, ado_work_item_id)
		DO UPDATE SET
			status = 'failed',
			error_message = EXCLUDED.error_message,
			retry_count = work_item_migrations.retry_count + 1,
			last_retry_at = NOW()
	`

	for adoWorkItemID, errorMsg := range failures {
		batch.Queue(query, migrationID, adoWorkItemID, errorMsg)
	}

	br := db.pool.SendBatch(ctx, batch)
	defer br.Close()

	for i := 0; i < len(failures); i++ {
		_, err := br.Exec()
		if err != nil {
			return fmt.Errorf("failed to execute batch failure record: %w", err)
		}
	}

	return nil
}

// RecordWorkItemFailure records a failed work item migration
func (db *DatabaseService) RecordWorkItemFailure(ctx context.Context, migrationID string, adoWorkItemID int, errorMsg string) error {
	query := `
		INSERT INTO work_item_migrations (
			migration_id, ado_work_item_id, status, error_message, retry_count, last_retry_at
		) VALUES ($1, $2, 'failed', $3, 1, NOW())
		ON CONFLICT (migration_id, ado_work_item_id)
		DO UPDATE SET
			status = 'failed',
			error_message = EXCLUDED.error_message,
			retry_count = work_item_migrations.retry_count + 1,
			last_retry_at = NOW()
	`

	_, err := db.pool.Exec(ctx, query, migrationID, adoWorkItemID, errorMsg)
	if err != nil {
		return fmt.Errorf("failed to record work item failure: %w", err)
	}

	return nil
}

// UpdateMigrationProgress updates the migration progress counters
func (db *DatabaseService) UpdateMigrationProgress(ctx context.Context, migrationID string, migrated, failed, skipped int) error {
	query := `
		UPDATE migrations
		SET migrated_items = $2,
		    failed_items = $3,
		    skipped_items = $4,
		    last_updated_at = NOW()
		WHERE id = $1
	`

	_, err := db.pool.Exec(ctx, query, migrationID, migrated, failed, skipped)
	if err != nil {
		return fmt.Errorf("failed to update migration progress: %w", err)
	}

	return nil
}

// CompleteMigration marks a migration as completed
func (db *DatabaseService) CompleteMigration(ctx context.Context, migrationID string, status string, errorMsg string) error {
	query := `
		UPDATE migrations
		SET status = $2,
		    completed_at = NOW(),
		    last_updated_at = NOW(),
		    error_message = $3
		WHERE id = $1
	`

	_, err := db.pool.Exec(ctx, query, migrationID, status, errorMsg)
	if err != nil {
		return fmt.Errorf("failed to complete migration: %w", err)
	}

	fmt.Printf("✅ Migration %s completed with status: %s\n", migrationID, status)
	return nil
}

// RecordRateLimitEvent records a rate limit incident
func (db *DatabaseService) RecordRateLimitEvent(ctx context.Context, migrationID string, service string, rateLimitType string, resetAt *time.Time, retryAfter int) error {
	query := `
		INSERT INTO rate_limit_events (
			migration_id, service, rate_limit_type, reset_at, retry_after_seconds, occurred_at
		) VALUES ($1, $2, $3, $4, $5, NOW())
	`

	_, err := db.pool.Exec(ctx, query, migrationID, service, rateLimitType, resetAt, retryAfter)
	if err != nil {
		return fmt.Errorf("failed to record rate limit event: %w", err)
	}

	fmt.Printf("⚠️  Rate limit recorded: %s %s (retry after %d seconds)\n", service, rateLimitType, retryAfter)
	return nil
}

// GetMigrationProgress retrieves the current migration progress
func (db *DatabaseService) GetMigrationProgress(ctx context.Context, migrationID string) (*MigrationRecord, error) {
	query := `
		SELECT id, migration_name, migration_namespace, source_organization, source_project,
		       source_team, target_owner, target_repository, status, total_items, migrated_items,
		       failed_items, skipped_items, started_at, completed_at, last_updated_at, error_message
		FROM migrations
		WHERE id = $1
	`

	var record MigrationRecord
	err := db.pool.QueryRow(ctx, query, migrationID).Scan(
		&record.ID,
		&record.MigrationName,
		&record.MigrationNamespace,
		&record.SourceOrganization,
		&record.SourceProject,
		&record.SourceTeam,
		&record.TargetOwner,
		&record.TargetRepository,
		&record.Status,
		&record.TotalItems,
		&record.MigratedItems,
		&record.FailedItems,
		&record.SkippedItems,
		&record.StartedAt,
		&record.CompletedAt,
		&record.LastUpdatedAt,
		&record.ErrorMessage,
	)

	if err != nil {
		return nil, fmt.Errorf("failed to get migration progress: %w", err)
	}

	return &record, nil
}

// RecordGitHubProject records a GitHub Project creation
func (db *DatabaseService) RecordGitHubProject(ctx context.Context, record *GitHubProjectRecord) error {
	query := `
		INSERT INTO github_projects (
			project_cr_name, project_cr_namespace, github_project_id,
			github_project_number, github_project_url, owner, project_name,
			template, public, repository, status, created_at, last_updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, NOW(), NOW())
		ON CONFLICT (project_cr_name, project_cr_namespace)
		DO UPDATE SET
			github_project_id = EXCLUDED.github_project_id,
			github_project_number = EXCLUDED.github_project_number,
			github_project_url = EXCLUDED.github_project_url,
			owner = EXCLUDED.owner,
			project_name = EXCLUDED.project_name,
			template = EXCLUDED.template,
			public = EXCLUDED.public,
			repository = EXCLUDED.repository,
			status = EXCLUDED.status,
			last_updated_at = NOW()
	`

	_, err := db.pool.Exec(ctx, query,
		record.ProjectCRName,
		record.ProjectCRNamespace,
		record.GithubProjectID,
		record.GithubProjectNumber,
		record.GithubProjectURL,
		record.Owner,
		record.ProjectName,
		record.Template,
		record.Public,
		record.Repository,
		record.Status,
	)

	if err != nil {
		return fmt.Errorf("failed to record GitHub project: %w", err)
	}

	fmt.Printf("📊 Recorded GitHub Project: %s (ID: %s)\n", record.ProjectName, record.GithubProjectID)
	return nil
}

// GetGitHubProjectByCRName retrieves a GitHub Project by CR name and namespace
func (db *DatabaseService) GetGitHubProjectByCRName(ctx context.Context, crName, crNamespace string) (*GitHubProjectRecord, error) {
	query := `
		SELECT id, project_cr_name, project_cr_namespace, github_project_id,
		       github_project_number, github_project_url, owner, project_name,
		       template, public, repository, status, created_at, last_updated_at, error_message
		FROM github_projects
		WHERE project_cr_name = $1 AND project_cr_namespace = $2
	`

	var record GitHubProjectRecord
	err := db.pool.QueryRow(ctx, query, crName, crNamespace).Scan(
		&record.ID,
		&record.ProjectCRName,
		&record.ProjectCRNamespace,
		&record.GithubProjectID,
		&record.GithubProjectNumber,
		&record.GithubProjectURL,
		&record.Owner,
		&record.ProjectName,
		&record.Template,
		&record.Public,
		&record.Repository,
		&record.Status,
		&record.CreatedAt,
		&record.LastUpdatedAt,
		&record.ErrorMessage,
	)

	if err == pgx.ErrNoRows {
		return nil, fmt.Errorf("GitHub project not found: %s/%s", crNamespace, crName)
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get GitHub project: %w", err)
	}

	return &record, nil
}

// UpdateMigrationWithProject links a migration to a GitHub Project
func (db *DatabaseService) UpdateMigrationWithProject(ctx context.Context, migrationID, projectID string) error {
	query := `
		UPDATE migrations
		SET github_project_id = $2,
		    last_updated_at = NOW()
		WHERE id = $1
	`

	_, err := db.pool.Exec(ctx, query, migrationID, projectID)
	if err != nil {
		return fmt.Errorf("failed to link migration to project: %w", err)
	}

	fmt.Printf("🔗 Linked migration %s to project %s\n", migrationID, projectID)
	return nil
}

// UpdateWorkItemWithProjectItem updates a work item record with project item details
func (db *DatabaseService) UpdateWorkItemWithProjectItem(ctx context.Context, workItemID, projectItemID, issueNodeID string) error {
	query := `
		UPDATE work_item_migrations
		SET github_project_item_id = $2,
		    github_issue_node_id = $3,
		    added_to_project_at = NOW()
		WHERE id = $1
	`

	_, err := db.pool.Exec(ctx, query, workItemID, projectItemID, issueNodeID)
	if err != nil {
		return fmt.Errorf("failed to update work item with project item: %w", err)
	}

	return nil
}
