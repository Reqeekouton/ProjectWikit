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

var (
	qArticleHead = register("ArticleHead", `
SELECT rev_number, created_at FROM web_articlelogentry
WHERE article_id = $1
ORDER BY rev_number DESC
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
