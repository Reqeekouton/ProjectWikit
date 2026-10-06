package db

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type ClaimOutcome int

const (
	ClaimMatched ClaimOutcome = iota
	ClaimMissing
	ClaimExpired
	ClaimWrong
	ClaimLocked
)

var qWikidotUserID = register("WikidotUserID", `
SELECT coalesce(wikidot_user_id, 0) FROM web_user WHERE id = $1`)

func (d *DB) WikidotUserID(ctx context.Context, userID int64) (int64, error) {
	var id int64
	err := d.pool.QueryRow(ctx, qWikidotUserID, userID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("wikidot id of user %d: %w", userID, err)
	}
	return id, nil
}

var qReserveClaimCode = register("ReserveClaimCode", `
INSERT INTO pwikit_claim_code (user_id, code_hash, sent_at, expires_at, attempts)
VALUES ($1, $2, $3, $4, 0)
ON CONFLICT (user_id) DO UPDATE
SET code_hash = EXCLUDED.code_hash, sent_at = EXCLUDED.sent_at,
	expires_at = EXCLUDED.expires_at, attempts = 0
WHERE pwikit_claim_code.sent_at < $5
RETURNING user_id`)

// The check and the write are one statement, so two sends racing for the same
// account cannot both get through.
func (d *DB) ReserveClaimCode(ctx context.Context, userID int64, hash string, now, expires, notBefore time.Time) (bool, error) {
	var id int64
	err := d.pool.QueryRow(ctx, qReserveClaimCode, userID, hash, now, expires, notBefore).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reserve claim code for user %d: %w", userID, err)
	}
	return true, nil
}

var qDropClaimCode = register("DropClaimCode", `
DELETE FROM pwikit_claim_code WHERE user_id = $1 AND code_hash = $2`)

func (d *DB) DropClaimCode(ctx context.Context, userID int64, hash string) error {
	if _, err := d.pool.Exec(ctx, qDropClaimCode, userID, hash); err != nil {
		return fmt.Errorf("drop claim code for user %d: %w", userID, err)
	}
	return nil
}

var (
	qLockClaimCode = register("LockClaimCode", `
SELECT code_hash, expires_at, attempts FROM pwikit_claim_code WHERE user_id = $1 FOR UPDATE`)
	qDeleteClaimCode = register("DeleteClaimCode", `
DELETE FROM pwikit_claim_code WHERE user_id = $1`)
	qMissClaimCode = register("MissClaimCode", `
UPDATE pwikit_claim_code SET attempts = attempts + 1 WHERE user_id = $1`)
)

func (d *DB) UseClaimCode(ctx context.Context, userID int64, hash string, now time.Time, maxAttempts int) (ClaimOutcome, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin claim code check: %w", err)
	}
	defer tx.Rollback(ctx)

	var stored string
	var expires time.Time
	var attempts int
	err = tx.QueryRow(ctx, qLockClaimCode, userID).Scan(&stored, &expires, &attempts)
	if errors.Is(err, pgx.ErrNoRows) {
		return ClaimMissing, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read claim code for user %d: %w", userID, err)
	}

	outcome := ClaimWrong
	switch {
	case !now.Before(expires):
		outcome = ClaimExpired
	case attempts >= maxAttempts:
		outcome = ClaimLocked
	case subtle.ConstantTimeCompare([]byte(stored), []byte(hash)) == 1:
		outcome = ClaimMatched
	}

	query := qDeleteClaimCode
	if outcome == ClaimWrong {
		query = qMissClaimCode
	}
	if _, err := tx.Exec(ctx, query, userID); err != nil {
		return 0, fmt.Errorf("settle claim code for user %d: %w", userID, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit claim code check: %w", err)
	}
	return outcome, nil
}
