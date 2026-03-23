package secrets

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// DBProvider implements SecretsProvider for SaaS mode, storing encrypted
// secrets in PostgreSQL using AES-256-GCM.
type DBProvider struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

// NewDBProvider creates a DBProvider with the given connection pool and a
// hex-encoded 32-byte AES-256 encryption key.
func NewDBProvider(pool *pgxpool.Pool, encryptionKeyHex string) (*DBProvider, error) {
	key, err := hex.DecodeString(encryptionKeyHex)
	if err != nil {
		return nil, fmt.Errorf("secrets: decode encryption key: %w", err)
	}
	if len(key) != 32 {
		return nil, fmt.Errorf("secrets: encryption key must be 32 bytes, got %d", len(key))
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("secrets: create AES cipher: %w", err)
	}

	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("secrets: create GCM: %w", err)
	}

	return &DBProvider{pool: pool, aead: aead}, nil
}

func (p *DBProvider) encrypt(data map[string]string) ([]byte, error) {
	plaintext, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("secrets: marshal data: %w", err)
	}

	nonce := make([]byte, p.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("secrets: generate nonce: %w", err)
	}

	// Prepend nonce to ciphertext so we can extract it on decryption.
	ciphertext := p.aead.Seal(nonce, nonce, plaintext, nil)
	return ciphertext, nil
}

func (p *DBProvider) decrypt(blob []byte) (map[string]string, error) {
	nonceSize := p.aead.NonceSize()
	if len(blob) < nonceSize {
		return nil, fmt.Errorf("secrets: ciphertext too short")
	}

	nonce, ciphertext := blob[:nonceSize], blob[nonceSize:]
	plaintext, err := p.aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("secrets: decrypt: %w", err)
	}

	var data map[string]string
	if err := json.Unmarshal(plaintext, &data); err != nil {
		return nil, fmt.Errorf("secrets: unmarshal decrypted data: %w", err)
	}
	return data, nil
}

// Store encrypts and persists secret data in the tenant_secrets table.
func (p *DBProvider) Store(ctx context.Context, tenantID, name, secretType string, data map[string]string) error {
	encrypted, err := p.encrypt(data)
	if err != nil {
		return err
	}

	query := `
		INSERT INTO tenant_secrets (tenant_id, name, secret_type, encrypted_data, created_at, updated_at)
		VALUES ($1, $2, $3, $4, NOW(), NOW())
		ON CONFLICT (tenant_id, name) DO UPDATE
			SET secret_type = EXCLUDED.secret_type,
			    encrypted_data = EXCLUDED.encrypted_data,
			    updated_at = NOW()
	`
	_, err = p.pool.Exec(ctx, query, tenantID, name, secretType, encrypted)
	if err != nil {
		return fmt.Errorf("secrets: store: %w", err)
	}
	return nil
}

// Get retrieves and decrypts secret data from the tenant_secrets table.
func (p *DBProvider) Get(ctx context.Context, tenantID, name string) (map[string]string, error) {
	query := `SELECT encrypted_data FROM tenant_secrets WHERE tenant_id = $1 AND name = $2`

	var encrypted []byte
	err := p.pool.QueryRow(ctx, query, tenantID, name).Scan(&encrypted)
	if err != nil {
		return nil, fmt.Errorf("secrets: get: %w", err)
	}

	return p.decrypt(encrypted)
}

// Delete removes a secret from the tenant_secrets table.
func (p *DBProvider) Delete(ctx context.Context, tenantID, name string) error {
	query := `DELETE FROM tenant_secrets WHERE tenant_id = $1 AND name = $2`
	_, err := p.pool.Exec(ctx, query, tenantID, name)
	if err != nil {
		return fmt.Errorf("secrets: delete: %w", err)
	}
	return nil
}

// List returns metadata for all secrets belonging to a tenant without
// decrypting their data.
func (p *DBProvider) List(ctx context.Context, tenantID string) ([]SecretMetadata, error) {
	query := `
		SELECT id, tenant_id, name, secret_type, created_at, updated_at
		FROM tenant_secrets
		WHERE tenant_id = $1
		ORDER BY name
	`

	rows, err := p.pool.Query(ctx, query, tenantID)
	if err != nil {
		return nil, fmt.Errorf("secrets: list: %w", err)
	}
	defer rows.Close()

	var result []SecretMetadata
	for rows.Next() {
		var m SecretMetadata
		var createdAt, updatedAt time.Time
		if err := rows.Scan(&m.ID, &m.TenantID, &m.Name, &m.SecretType, &createdAt, &updatedAt); err != nil {
			return nil, fmt.Errorf("secrets: list scan: %w", err)
		}
		m.CreatedAt = createdAt
		m.UpdatedAt = updatedAt
		result = append(result, m)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("secrets: list rows: %w", err)
	}

	return result, nil
}

// ResolveK8sSecretName returns a deterministic Kubernetes Secret name and
// namespace for a given tenant secret. The orchestrator is responsible for
// actually creating the K8s Secret with the decrypted data.
func (p *DBProvider) ResolveK8sSecretName(_ context.Context, tenantID, name string) (string, string, error) {
	secretName := fmt.Sprintf("reposhift-%s-%s", tenantID, name)
	namespace := fmt.Sprintf("tenant-%s", tenantID)
	return secretName, namespace, nil
}
