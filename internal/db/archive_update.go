package db

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

type ArticleHead struct {
	RevNumber int
	At        time.Time
}

type ImportUpdate struct {
	Title       string
	Locked      bool
	UpdatedAt   time.Time
	Indexed     string
	Revisions   []ImportRevision
	ReplaceTags bool
	TagIDs      []int64
}

var (
	qArticleHead = register("ArticleHead", `
SELECT rev_number, created_at FROM web_articlelogentry
WHERE article_id = $1
ORDER BY rev_number DESC
LIMIT 1`)

	qUpdateImportedArticle = register("UpdateImportedArticle", `
UPDATE web_article SET title = $3, locked = $4, updated_at = $5
WHERE id = $1 AND site_id = $2`)

	qArticleFileNames = register("ArticleFileNames", `
SELECT name FROM web_file WHERE article_id = $1`)

	qThreadPostKeys = register("ThreadPostKeys", `
SELECT id, created_at, author_id FROM web_forumpost WHERE thread_id = $1 ORDER BY id`)

	qImportedSection = register("ImportedSection", `
SELECT id FROM web_forumsection WHERE site_id = $1 AND name = $2 ORDER BY id LIMIT 1`)

	qImportedCategory = register("ImportedCategory", `
SELECT id FROM web_forumcategory
WHERE section_id = $1 AND name = $2
  AND section_id IN (SELECT id FROM web_forumsection WHERE site_id = $3)
ORDER BY id
LIMIT 1`)
)

func (d *DB) ArticleHead(ctx context.Context, articleID int64) (ArticleHead, error) {
	var h ArticleHead
	err := d.pool.QueryRow(ctx, qArticleHead, articleID).Scan(&h.RevNumber, &h.At)
	if errors.Is(err, pgx.ErrNoRows) {
		return h, ErrNotFound
	}
	if err != nil {
		return h, fmt.Errorf("read the newest revision of %d: %w", articleID, err)
	}
	return h, nil
}

func writeImportedRevisions(ctx context.Context, tx pgx.Tx, articleID int64, title string, revs []ImportRevision) error {
	for _, rev := range revs {
		kind := LogWikidot
		meta := map[string]any{}
		if rev.Source != nil {
			var versionID int64
			err := tx.QueryRow(ctx, qInsertArticleVersion, articleID, *rev.Source, rev.At).Scan(&versionID)
			if err != nil {
				return fmt.Errorf("import a version of %d: %w", articleID, err)
			}
			kind = LogSource
			meta["version_id"] = versionID
			if rev.IsNew {
				kind = LogNew
				meta["title"] = title
			}
		}
		encoded, err := json.Marshal(meta)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, qImportLogEntry, articleID, rev.UserID, kind, encoded, rev.Comment, rev.At, rev.Number); err != nil {
			return fmt.Errorf("import a revision of %d: %w", articleID, err)
		}
	}
	return nil
}

func (d *DB) UpdateImportedArticle(ctx context.Context, siteID, articleID int64, u ImportUpdate) error {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin updating %d: %w", articleID, err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	if err := writeImportedRevisions(ctx, tx, articleID, u.Title, u.Revisions); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, qUpdateImportedArticle, articleID, siteID, u.Title, u.Locked, u.UpdatedAt); err != nil {
		return fmt.Errorf("update %d: %w", articleID, err)
	}
	if u.Indexed != "" {
		text := u.Title + "\n\n" + u.Indexed
		tag, err := tx.Exec(ctx, qUpdateSearchIndex, articleID, text, text)
		if err != nil {
			return fmt.Errorf("update search index of %d: %w", articleID, err)
		}
		if tag.RowsAffected() == 0 {
			if _, err := tx.Exec(ctx, qInsertSearchIndex, articleID, text, text); err != nil {
				return fmt.Errorf("write search index of %d: %w", articleID, err)
			}
		}
	}
	if u.ReplaceTags {
		keep := u.TagIDs
		if keep == nil {
			keep = []int64{}
		}
		if _, err := tx.Exec(ctx, qDropArticleTags, articleID, keep); err != nil {
			return fmt.Errorf("drop tags of %d: %w", articleID, err)
		}
		for _, tagID := range u.TagIDs {
			if _, err := tx.Exec(ctx, qImportArticleTag, articleID, tagID); err != nil {
				return fmt.Errorf("tag %d: %w", articleID, err)
			}
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit updating %d: %w", articleID, err)
	}
	return nil
}

func (d *DB) ArticleFileNames(ctx context.Context, articleID int64) (map[string]bool, error) {
	rows, err := d.pool.Query(ctx, qArticleFileNames, articleID)
	if err != nil {
		return nil, fmt.Errorf("list attachments of %d: %w", articleID, err)
	}
	defer rows.Close()
	out := map[string]bool{}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out[name] = true
	}
	return out, rows.Err()
}

type PostKey struct {
	ID       int64
	At       time.Time
	AuthorID *int64
}

func (d *DB) ThreadPostKeys(ctx context.Context, threadID int64) ([]PostKey, error) {
	rows, err := d.pool.Query(ctx, qThreadPostKeys, threadID)
	if err != nil {
		return nil, fmt.Errorf("list posts of %d: %w", threadID, err)
	}
	defer rows.Close()
	var out []PostKey
	for rows.Next() {
		var k PostKey
		if err := rows.Scan(&k.ID, &k.At, &k.AuthorID); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

func (d *DB) AppendImportedPost(ctx context.Context, threadID int64, post ImportPost, replyTo *int64) (int64, *Renumbered, error) {
	tx, err := d.pool.Begin(ctx)
	if err != nil {
		return 0, nil, fmt.Errorf("begin importing a post: %w", err)
	}
	defer tx.Rollback(context.WithoutCancel(ctx))

	updated := post.CreatedAt
	if n := len(post.Versions); n > 0 {
		updated = post.Versions[n-1].At
	}
	postID, moved, err := insertAt(ctx, tx, RenumberedPost, post.WantID, qImportPostAt, qImportPost,
		threadID, post.Name, post.AuthorID, replyTo, post.CreatedAt, updated)
	if err != nil {
		return 0, nil, fmt.Errorf("import a post of %d: %w", threadID, err)
	}
	for _, version := range post.Versions {
		if _, err := tx.Exec(ctx, qImportPostVersion, postID, version.Source, version.AuthorID, version.At); err != nil {
			return 0, nil, fmt.Errorf("import a post version of %d: %w", threadID, err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, nil, fmt.Errorf("commit a post of %d: %w", threadID, err)
	}
	return postID, moved, nil
}

func (d *DB) ImportedSection(ctx context.Context, siteID int64, name string) (int64, error) {
	return d.oneID(ctx, qImportedSection, siteID, name)
}

func (d *DB) ImportedCategory(ctx context.Context, siteID, sectionID int64, name string) (int64, error) {
	return d.oneID(ctx, qImportedCategory, sectionID, name, siteID)
}
