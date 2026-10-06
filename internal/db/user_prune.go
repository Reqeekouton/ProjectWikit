package db

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

type UnusedUser struct {
	ID              int64
	WikidotUsername string
	DisplayName     string
}

var qUserMentions = register("UserMentions", `
SELECT DISTINCT trim(m[1])
FROM (
	SELECT source FROM web_articleversion WHERE source ILIKE '%[[%user%'
	UNION ALL
	SELECT source FROM web_forumpostversion WHERE source ILIKE '%[[%user%'
) s,
LATERAL regexp_matches(s.source, '\[\[\*?user\s+([^\]]+)\]\]', 'gi') AS m`)

// Pages and posts name accounts in [[user]] blocks, which hold no reference
// the database can see.
func (d *DB) UserMentions(ctx context.Context) ([]string, error) {
	rows, err := d.pool.Query(ctx, qUserMentions)
	if err != nil {
		return nil, fmt.Errorf("list user mentions: %w", err)
	}
	return pgx.CollectRows(rows, pgx.RowTo[string])
}

type querier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

func unreferencedWikidot(ctx context.Context, q querier) (string, error) {
	rows, err := q.Query(ctx, qUserReferences)
	if err != nil {
		return "", fmt.Errorf("list references to accounts: %w", err)
	}
	defer rows.Close()
	var b strings.Builder
	b.WriteString("u.type = 'wikidot'")
	for rows.Next() {
		var schema, table, column string
		if err := rows.Scan(&schema, &table, &column); err != nil {
			return "", err
		}
		fmt.Fprintf(&b, " AND NOT EXISTS (SELECT 1 FROM %s x WHERE x.%s = u.id)",
			pgx.Identifier{schema, table}.Sanitize(), pgx.Identifier{column}.Sanitize())
	}
	if err := rows.Err(); err != nil {
		return "", fmt.Errorf("list references to accounts: %w", err)
	}
	return b.String(), nil
}

func (d *DB) UnreferencedWikidotUsers(ctx context.Context) ([]UnusedUser, error) {
	where, err := unreferencedWikidot(ctx, d.pool)
	if err != nil {
		return nil, err
	}
	rows, err := d.pool.Query(ctx, `
SELECT u.id, COALESCE(u.wikidot_username::text, ''), COALESCE(u.display_name, '')
FROM web_user u WHERE `+where+` ORDER BY u.id`)
	if err != nil {
		return nil, fmt.Errorf("list unreferenced accounts: %w", err)
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (UnusedUser, error) {
		var u UnusedUser
		err := row.Scan(&u.ID, &u.WikidotUsername, &u.DisplayName)
		return u, err
	})
}

func (d *DB) DeleteUnreferencedWikidotUsers(ctx context.Context, ids []int64) (int64, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin deleting accounts: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	where, err := unreferencedWikidot(ctx, tx)
	if err != nil {
		return 0, err
	}
	tag, err := tx.Exec(ctx, `DELETE FROM web_user u WHERE u.id = ANY($1) AND `+where, ids)
	if err != nil {
		return 0, fmt.Errorf("delete accounts: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit deleting accounts: %w", err)
	}
	return tag.RowsAffected(), nil
}
