package archive

import (
	"context"
	"errors"
	"time"

	"github.com/WikitTeam/ProjectWikit/internal/db"
)

type Backfill struct {
	Revisions int64
	Authors   int64
	Votes     int64
	Files     int64
	Threads   int64
	Posts     int64
	Versions  int64
}

func BackfillUsers(ctx context.Context, d *db.DB, siteID int64, a *Archive, slug string, opts Options) (Backfill, error) {
	var out Backfill
	im := &importer{db: d, archive: a, slug: slug, siteID: siteID, opts: opts}

	pages, err := a.Pages(slug)
	if err != nil {
		return out, err
	}
	if im.users, err = im.importUsers(ctx, pages); err != nil {
		return out, err
	}

	byThread := map[int64]int64{}
	for _, page := range pages {
		article, err := d.ArticleByName(ctx, siteID, page.Name)
		if errors.Is(err, db.ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}
		byThread[page.ThreadID] = article.ID
		if err := im.backfillPage(ctx, page, article.ID, &out); err != nil {
			return out, err
		}
	}
	delete(byThread, 0)
	if err := im.backfillForum(ctx, byThread, &out); err != nil {
		return out, err
	}
	return out, nil
}

func (im *importer) knownUser(wikidotID int64) (int64, bool) {
	local, ok := im.users[wikidotID]
	return local, ok
}

func (im *importer) backfillPage(ctx context.Context, page Page, articleID int64, out *Backfill) error {
	for _, rev := range page.Revisions {
		user, ok := im.knownUser(rev.Author)
		if !ok {
			continue
		}
		n, err := im.db.FillRevisionUser(ctx, articleID, rev.Number, user)
		if err != nil {
			return err
		}
		out.Revisions += n
	}
	if len(page.Revisions) > 0 {
		if user, ok := im.knownUser(page.Revisions[len(page.Revisions)-1].Author); ok {
			n, err := im.db.FillArticleAuthor(ctx, articleID, user)
			if err != nil {
				return err
			}
			out.Authors += n
		}
	}
	if im.opts.Votes {
		for _, vote := range page.Votings {
			user, ok := im.knownUser(vote.UserID)
			if !ok {
				continue
			}
			n, err := im.db.AddImportedVote(ctx, articleID, user, vote.Value)
			if err != nil {
				return err
			}
			out.Votes += n
		}
	}
	for _, file := range page.Files {
		user, ok := im.knownUser(file.Author)
		if !ok {
			continue
		}
		n, err := im.db.FillFileAuthor(ctx, articleID, file.Name, time.Unix(file.Stamp, 0).UTC(), user)
		if err != nil {
			return err
		}
		out.Files += n
	}
	return nil
}

func (im *importer) backfillForum(ctx context.Context, byArchiveThread map[int64]int64, out *Backfill) error {
	categories, err := im.archive.Categories(im.slug)
	if err != nil {
		return err
	}
	for _, category := range categories {
		threads, err := im.archive.Threads(im.slug, category.ID)
		if err != nil {
			return err
		}
		for _, thread := range threads {
			threadID, err := im.findThread(ctx, category, thread, byArchiveThread)
			if errors.Is(err, db.ErrNotFound) {
				continue
			}
			if err != nil {
				return err
			}
			if err := im.backfillThread(ctx, thread, threadID, out); err != nil {
				return err
			}
		}
	}
	return nil
}

func (im *importer) findThread(ctx context.Context, category Category, thread Thread, byArchiveThread map[int64]int64) (int64, error) {
	if article, ok := byArchiveThread[thread.ID]; ok {
		return im.db.ImportedCommentThread(ctx, im.siteID, article)
	}
	return im.db.ImportedThread(ctx, im.siteID, importedSection, category.Title, thread.Title,
		time.Unix(thread.Started, 0).UTC())
}

func (im *importer) backfillThread(ctx context.Context, thread Thread, threadID int64, out *Backfill) error {
	if user, ok := im.knownUser(started(thread)); ok {
		n, err := im.db.FillThreadAuthor(ctx, im.siteID, threadID, user)
		if err != nil {
			return err
		}
		out.Threads += n
	}

	posts, _ := im.flatten(thread.Posts, -1, nil, nil, nil)
	stored, err := im.db.ThreadPostTimes(ctx, threadID)
	if err != nil {
		return err
	}
	for i, post := range posts {
		if i >= len(stored) || !stored[i].At.Equal(post.CreatedAt) {
			continue
		}
		if post.AuthorID != nil {
			n, err := im.db.FillPostAuthor(ctx, stored[i].ID, *post.AuthorID)
			if err != nil {
				return err
			}
			out.Posts += n
		}
		if err := im.backfillVersions(ctx, post, stored[i].ID, out); err != nil {
			return err
		}
	}
	return nil
}

func (im *importer) backfillVersions(ctx context.Context, post db.ImportPost, postID int64, out *Backfill) error {
	stored, err := im.db.PostVersionTimes(ctx, postID)
	if err != nil {
		return err
	}
	for i, version := range post.Versions {
		if i >= len(stored) || !stored[i].At.Equal(version.At) {
			continue
		}
		if version.AuthorID == nil {
			continue
		}
		n, err := im.db.FillPostVersionAuthor(ctx, stored[i].ID, *version.AuthorID)
		if err != nil {
			return err
		}
		out.Versions += n
	}
	return nil
}
