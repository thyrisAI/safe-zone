package policy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type Compiler struct {
	repository Repository
	now        func() time.Time
}

func NewCompiler(repository Repository) (*Compiler, error) {
	if repository == nil {
		return nil, errors.New("policy repository is required")
	}
	return &Compiler{repository: repository, now: time.Now}, nil
}

// Compile converts one validated snapshot into an immutable, versioned
// compiled snapshot. Reference resolution, version allocation and the state
// transition share one database transaction.
func (c *Compiler) Compile(ctx context.Context, snapshotID int64) error {
	if snapshotID <= 0 {
		return fmt.Errorf("compile policy snapshot: invalid snapshot id %d", snapshotID)
	}
	return c.repository.WithinTransaction(ctx, func(transaction Transaction) error {
		snapshot, err := transaction.SnapshotForUpdate(ctx, snapshotID)
		if err != nil {
			return fmt.Errorf("load validated snapshot: %w", err)
		}
		if snapshot.Status != StatusValidated {
			return fmt.Errorf("%w: snapshot %d has status %q, want %q", ErrInvalidTransition, snapshotID, snapshot.Status, StatusValidated)
		}

		resolvedDefinition, err := transaction.ResolveReferences(ctx, snapshot.Definition)
		if err != nil {
			return fmt.Errorf("resolve policy references: %w", err)
		}
		canonical, err := CanonicalDefinition(resolvedDefinition)
		if err != nil {
			return err
		}
		integrityHash := hashCanonicalDefinition(canonical)

		version, err := transaction.NextVersion(ctx, snapshot.PolicyID)
		if err != nil {
			return fmt.Errorf("allocate policy version: %w", err)
		}
		if err := transaction.MarkCompiled(
			ctx,
			snapshot.ID,
			resolvedDefinition,
			version,
			integrityHash,
			c.now().UTC(),
		); err != nil {
			return fmt.Errorf("persist compiled snapshot: %w", err)
		}
		return nil
	})
}

func DefinitionIntegrityHash(definition PolicyDefinition) (string, error) {
	canonical, err := CanonicalDefinition(definition)
	if err != nil {
		return "", err
	}
	return hashCanonicalDefinition(canonical), nil
}

func hashCanonicalDefinition(canonical []byte) string {
	digest := sha256.Sum256(canonical)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func CanonicalDefinition(definition PolicyDefinition) ([]byte, error) {
	encoded, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("canonicalize policy definition: %w", err)
	}
	return encoded, nil
}
