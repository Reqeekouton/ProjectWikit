package db

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

type MergeMove struct {
	Table   string
	Column  string
	Moved   int64
	Dropped int64
}

var ErrMergeSame = errors.New("the two accounts are the same account")

var ErrMergeTwoWikidot = errors.New("both accounts carry a Wikidot identity")

var qUserByWikidotAlias = register("UserByWikidotAlias", `
SELECT `+userColumns+`
FROM web_user
WHERE lower(wikidot_username) = lower($1)
ORDER BY id
LIMIT 1`)

func (d *DB) UserByWikidotAlias(ctx context.Context, name string) (*User, error) {
	return d.scanUser(ctx, qUserByWikidotAlias, name)
}

var qUserReferences = register("UserReferences", `
SELECT n.nspname, t.relname, a.attname
FROM pg_constraint c
JOIN pg_class t ON t.oid = c.conrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
JOIN pg_attribute a ON a.attrelid = c.conrelid AND a.attnum = c.conkey[1]
WHERE c.contype = 'f' AND c.confrelid = 'web_user'::regclass AND array_length(c.conkey, 1) = 1
ORDER BY t.relname, a.attname`)

var qUniqueColumns = register("UniqueColumns", `
SELECT array_agg(a.attname ORDER BY k.ord)
FROM pg_index i
JOIN pg_class t ON t.oid = i.indrelid
JOIN pg_namespace n ON n.oid = t.relnamespace
CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = k.attnum
WHERE n.nspname = $1 AND t.relname = $2 AND i.indisunique AND i.indpred IS NULL
  AND NOT (0 = ANY(i.indkey))
GROUP BY i.indexrelid`)

var qMergeUserRows = register("MergeUserRows", `
SELECT wikidot_username, wikidot_user_id, display_name, avatar, bio
FROM web_user WHERE id = $1`)

var qDeleteMergedUser = register("DeleteMergedUser", `DELETE FROM web_user WHERE id = $1`)

var qDropSelfBlocks = register("DropSelfBlocks", `
DELETE FROM web_directmessageblock WHERE blocker_id = $1 AND blocked_id = $1`)

var qFillMergedUser = register("FillMergedUser", `
UPDATE web_user SET
	wikidot_username = COALESCE(wikidot_username, $2::text),
	wikidot_user_id = COALESCE(wikidot_user_id, $3::bigint),
	display_name = COALESCE(NULLIF(display_name, ''), $4::text),
	avatar = COALESCE(NULLIF(avatar, ''), $5::text),
	bio = CASE WHEN bio = '' THEN $6::text ELSE bio END
WHERE id = $1`)

type mergeRow struct {
	wikidotUsername *string
	wikidotUserID   *int64
	displayName     *string
	avatar          *string
	bio             string
}

func readMergeRow(ctx context.Context, tx pgx.Tx, id int64) (mergeRow, error) {
	var r mergeRow
	err := tx.QueryRow(ctx, qMergeUserRows, id).Scan(&r.wikidotUsername,
		&r.wikidotUserID, &r.displayName, &r.avatar, &r.bio)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, ErrNotFound
	}
	if err != nil {
		return r, fmt.Errorf("read account %d: %w", id, err)
	}
	return r, nil
}

func (d *DB) MergeUsers(ctx context.Context, from, into int64) ([]MergeMove, error) {
	if from == into {
		return nil, ErrMergeSame
	}
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin merging accounts: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	source, err := readMergeRow(ctx, tx, from)
	if err != nil {
		return nil, err
	}
	target, err := readMergeRow(ctx, tx, into)
	if err != nil {
		return nil, err
	}
	if source.wikidotUserID != nil && target.wikidotUserID != nil && *source.wikidotUserID != *target.wikidotUserID {
		return nil, ErrMergeTwoWikidot
	}

	type ref struct{ schema, table, column string }
	rows, err := tx.Query(ctx, qUserReferences)
	if err != nil {
		return nil, fmt.Errorf("list references to accounts: %w", err)
	}
	var refs []ref
	for rows.Next() {
		var r ref
		if err := rows.Scan(&r.schema, &r.table, &r.column); err != nil {
			rows.Close()
			return nil, err
		}
		refs = append(refs, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list references to accounts: %w", err)
	}

	var moves []MergeMove
	for _, r := range refs {
		move, err := moveReferences(ctx, tx, r.schema, r.table, r.column, from, into)
		if err != nil {
			return nil, err
		}
		if move.Moved > 0 || move.Dropped > 0 {
			moves = append(moves, move)
		}
	}
	if _, err := tx.Exec(ctx, qDropSelfBlocks, into); err != nil {
		return nil, fmt.Errorf("drop self blocks: %w", err)
	}

	if _, err := tx.Exec(ctx, qDeleteMergedUser, from); err != nil {
		return nil, fmt.Errorf("delete account %d: %w", from, err)
	}
	if _, err := tx.Exec(ctx, qFillMergedUser, into, source.wikidotUsername, source.wikidotUserID,
		source.displayName, source.avatar, source.bio); err != nil {
		return nil, fmt.Errorf("fill account %d: %w", into, err)
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit merging accounts: %w", err)
	}
	return moves, nil
}

func moveReferences(ctx context.Context, tx pgx.Tx, schema, table, column string, from, into int64) (MergeMove, error) {
	move := MergeMove{Table: table, Column: column}
	name := pgx.Identifier{schema, table}.Sanitize()
	col := pgx.Identifier{column}.Sanitize()

	rows, err := tx.Query(ctx, qUniqueColumns, schema, table)
	if err != nil {
		return move, fmt.Errorf("list unique keys of %s: %w", table, err)
	}
	var keys [][]string
	for rows.Next() {
		var cols []string
		if err := rows.Scan(&cols); err != nil {
			rows.Close()
			return move, err
		}
		if slices.Contains(cols, column) {
			keys = append(keys, cols)
		}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return move, fmt.Errorf("list unique keys of %s: %w", table, err)
	}

	for _, key := range keys {
		sql := "DELETE FROM " + name + " a USING " + name + " b WHERE a." + col + " = $1 AND b." + col + " = $2"
		for _, other := range key {
			if other == column {
				continue
			}
			o := pgx.Identifier{other}.Sanitize()
			sql += " AND a." + o + " IS NOT DISTINCT FROM b." + o
		}
		tag, err := tx.Exec(ctx, sql, from, into)
		if err != nil {
			return move, fmt.Errorf("drop duplicate %s.%s rows: %w", table, column, err)
		}
		move.Dropped += tag.RowsAffected()
	}

	tag, err := tx.Exec(ctx, "UPDATE "+name+" SET "+col+" = $2 WHERE "+col+" = $1", from, into)
	if err != nil {
		return move, fmt.Errorf("move %s.%s: %w", table, column, err)
	}
	move.Moved = tag.RowsAffected()
	return move, nil
}
