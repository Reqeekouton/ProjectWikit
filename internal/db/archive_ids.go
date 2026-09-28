package db

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/jackc/pgx/v5"
)

const (
	RenumberedCategory = "category"
	RenumberedThread   = "thread"
	RenumberedPost     = "post"
)

type Renumbered struct {
	Kind string
	From int64
	To   int64
}

var (
	qImportForumCategoryAt = register("ImportForumCategoryAt", `
INSERT INTO web_forumcategory (id, name, description, "order", is_for_comments, section_id)
SELECT $7, $1, $2, $3, $4, $5
WHERE EXISTS (SELECT 1 FROM web_forumsection WHERE id = $5 AND site_id = $6)
ON CONFLICT (id) DO NOTHING
RETURNING id`)

	qImportThreadAt = register("ImportThreadAt", `
INSERT INTO web_forumthread (id, site_id, category_id, article_id, name, description, author_id,
	created_at, updated_at, is_pinned, is_locked)
VALUES ($11, $1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
ON CONFLICT (id) DO NOTHING
RETURNING id`)

	qImportPostAt = register("ImportPostAt", `
INSERT INTO web_forumpost (id, thread_id, name, author_id, reply_to_id, created_at, updated_at)
VALUES ($7, $1, $2, $3, $4, $5, $6)
ON CONFLICT (id) DO NOTHING
RETURNING id`)

	qBumpForumSequences = register("BumpForumSequences", `
SELECT
	setval(c, greatest(coalesce(pg_sequence_last_value(c::regclass), 1), (SELECT coalesce(max(id), 1) FROM web_forumcategory))),
	setval(t, greatest(coalesce(pg_sequence_last_value(t::regclass), 1), (SELECT coalesce(max(id), 1) FROM web_forumthread))),
	setval(p, greatest(coalesce(pg_sequence_last_value(p::regclass), 1), (SELECT coalesce(max(id), 1) FROM web_forumpost)))
FROM (SELECT
	pg_get_serial_sequence('web_forumcategory', 'id') AS c,
	pg_get_serial_sequence('web_forumthread', 'id') AS t,
	pg_get_serial_sequence('web_forumpost', 'id') AS p) AS s`)
)

type rowQuerier interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func insertAt(ctx context.Context, q rowQuerier, kind string, want int64, at, fresh string, args ...any) (int64, *Renumbered, error) {
	var id int64
	if want > 0 {
		err := q.QueryRow(ctx, at, append(slices.Clone(args), want)...).Scan(&id)
		if err == nil {
			return id, nil, nil
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, err
		}
	}
	if err := q.QueryRow(ctx, fresh, args...).Scan(&id); err != nil {
		return 0, nil, err
	}
	if want > 0 {
		return id, &Renumbered{Kind: kind, From: want, To: id}, nil
	}
	return id, nil, nil
}

func (d *DB) BumpForumSequences(ctx context.Context) error {
	if _, err := d.pool.Exec(ctx, qBumpForumSequences); err != nil {
		return fmt.Errorf("move the forum sequences past the imported numbers: %w", err)
	}
	return nil
}
