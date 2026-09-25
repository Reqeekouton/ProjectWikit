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
