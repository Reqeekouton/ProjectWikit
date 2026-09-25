package db

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

var (
	qFillRevisionUser = register("FillRevisionUser", `
UPDATE web_articlelogentry SET user_id = $3
WHERE article_id = $1 AND rev_number = $2 AND user_id IS NULL`)

	qFillArticleAuthor = register("FillArticleAuthor", `
INSERT INTO web_article_authors (article_id, user_id)
SELECT $1, $2
WHERE NOT EXISTS (SELECT 1 FROM web_article_authors WHERE article_id = $1)`)

	qFillFileAuthor = register("FillFileAuthor", `
UPDATE web_file SET author_id = $4
WHERE article_id = $1 AND name = $2 AND created_at = $3 AND author_id IS NULL`)

	qImportedCommentThread = register("ImportedCommentThread", `
SELECT id FROM web_forumthread
WHERE site_id = $1 AND article_id = $2
ORDER BY id
LIMIT 1`)

	qImportedThread = register("ImportedThread", `
SELECT t.id
FROM web_forumthread t
JOIN web_forumcategory c ON c.id = t.category_id
JOIN web_forumsection s ON s.id = c.section_id
WHERE t.site_id = $1 AND s.name = $2 AND c.name = $3 AND t.name = $4 AND t.created_at = $5
ORDER BY t.id
LIMIT 1`)

	qFillThreadAuthor = register("FillThreadAuthor", `
UPDATE web_forumthread SET author_id = $3
WHERE id = $1 AND site_id = $2 AND author_id IS NULL`)

	qThreadPostTimes = register("ThreadPostTimes", `
SELECT id, created_at FROM web_forumpost WHERE thread_id = $1 ORDER BY id`)

	qFillPostAuthor = register("FillPostAuthor", `
UPDATE web_forumpost SET author_id = $2 WHERE id = $1 AND author_id IS NULL`)

	qPostVersionTimes = register("PostVersionTimes", `
SELECT id, created_at FROM web_forumpostversion WHERE post_id = $1 ORDER BY id`)

	qFillPostVersionAuthor = register("FillPostVersionAuthor", `
UPDATE web_forumpostversion SET author_id = $2 WHERE id = $1 AND author_id IS NULL`)
)

type Stamped struct {
	ID int64
	At time.Time
}

func (d *DB) exec(ctx context.Context, what, sql string, args ...any) (int64, error) {
	tag, err := d.pool.Exec(ctx, sql, args...)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	return tag.RowsAffected(), nil
}

func (d *DB) FillRevisionUser(ctx context.Context, articleID int64, revNumber int, userID int64) (int64, error) {
	return d.exec(ctx, "fill a revision author", qFillRevisionUser, articleID, revNumber, userID)
}

func (d *DB) FillArticleAuthor(ctx context.Context, articleID, userID int64) (int64, error) {
	return d.exec(ctx, "fill a page author", qFillArticleAuthor, articleID, userID)
}

func (d *DB) AddImportedVote(ctx context.Context, articleID, userID int64, rate float64) (int64, error) {
	return d.exec(ctx, "add an imported vote", qImportVote, articleID, userID, rate)
}

func (d *DB) FillFileAuthor(ctx context.Context, articleID int64, name string, at time.Time, userID int64) (int64, error) {
	return d.exec(ctx, "fill an attachment author", qFillFileAuthor, articleID, name, at, userID)
}

func (d *DB) ImportedCommentThread(ctx context.Context, siteID, articleID int64) (int64, error) {
	return d.oneID(ctx, qImportedCommentThread, siteID, articleID)
}

func (d *DB) ImportedThread(ctx context.Context, siteID int64, section, category, name string, at time.Time) (int64, error) {
	return d.oneID(ctx, qImportedThread, siteID, section, category, name, at)
}

func (d *DB) oneID(ctx context.Context, sql string, args ...any) (int64, error) {
	var id int64
	err := d.pool.QueryRow(ctx, sql, args...).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, fmt.Errorf("find an imported thread: %w", err)
	}
	return id, nil
}

func (d *DB) FillThreadAuthor(ctx context.Context, siteID, threadID, userID int64) (int64, error) {
	return d.exec(ctx, "fill a thread author", qFillThreadAuthor, threadID, siteID, userID)
}

func (d *DB) ThreadPostTimes(ctx context.Context, threadID int64) ([]Stamped, error) {
	return d.stamped(ctx, qThreadPostTimes, threadID)
}

func (d *DB) FillPostAuthor(ctx context.Context, postID, userID int64) (int64, error) {
	return d.exec(ctx, "fill a post author", qFillPostAuthor, postID, userID)
}

func (d *DB) PostVersionTimes(ctx context.Context, postID int64) ([]Stamped, error) {
	return d.stamped(ctx, qPostVersionTimes, postID)
}

func (d *DB) FillPostVersionAuthor(ctx context.Context, versionID, userID int64) (int64, error) {
	return d.exec(ctx, "fill a post version author", qFillPostVersionAuthor, versionID, userID)
}

func (d *DB) stamped(ctx context.Context, sql string, id int64) ([]Stamped, error) {
	rows, err := d.pool.Query(ctx, sql, id)
	if err != nil {
		return nil, fmt.Errorf("list imported rows of %d: %w", id, err)
	}
	defer rows.Close()
	var out []Stamped
	for rows.Next() {
		var s Stamped
		if err := rows.Scan(&s.ID, &s.At); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
