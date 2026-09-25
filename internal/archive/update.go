package archive

import (
	"context"
	"errors"
	"time"

	"github.com/WikitTeam/ProjectWikit/internal/db"
)

type UpdatePlan struct {
	New     int
	Current int
	Update  []string
	Edited  []string
}

type UpdateResult struct {
	Pages      int
	Revisions  int
	Edited     []string
	Votes      int64
	Files      int
	Categories int
	Threads    int
	Posts      int
}

type pageState int

const (
	stateCurrent pageState = iota
	stateNewer
	stateEdited
)

func PlanUpdate(ctx context.Context, d *db.DB, siteID int64, a *Archive, slug string) (UpdatePlan, error) {
	var plan UpdatePlan
	pages, err := a.Pages(slug)
	if err != nil {
		return plan, err
	}
	for _, page := range pages {
		article, err := d.ArticleByName(ctx, siteID, page.Name)
		if errors.Is(err, db.ErrNotFound) {
			plan.New++
			continue
		}
		if err != nil {
			return plan, err
		}
		state, _, err := stateOf(ctx, d, article.ID, page)
		if err != nil {
			return plan, err
		}
		switch state {
		case stateNewer:
			plan.Update = append(plan.Update, page.Name)
		case stateEdited:
			plan.Edited = append(plan.Edited, page.Name)
		default:
			plan.Current++
		}
	}
	return plan, nil
}

func stateOf(ctx context.Context, d *db.DB, articleID int64, page Page) (pageState, []Revision, error) {
	head, err := d.ArticleHead(ctx, articleID)
	if errors.Is(err, db.ErrNotFound) {
		return stateEdited, nil, nil
	}
	if err != nil {
		return stateCurrent, nil, err
	}
	matched := false
	var newer []Revision
	for _, rev := range page.Revisions {
		if rev.Number == head.RevNumber && time.Unix(rev.Stamp, 0).UTC().Equal(head.At) {
			matched = true
		}
		if rev.Number > head.RevNumber {
			newer = append(newer, rev)
		}
	}
	switch {
	case !matched:
		return stateEdited, nil, nil
	case len(newer) == 0:
		return stateCurrent, nil, nil
	}
	return stateNewer, newer, nil
}

func ApplyUpdate(ctx context.Context, d *db.DB, siteID int64, a *Archive, slug string, opts Options) (UpdateResult, error) {
	var out UpdateResult
	im := &importer{db: d, archive: a, slug: slug, siteID: siteID, opts: opts}

	pages, err := a.Pages(slug)
	if err != nil {
		return out, err
	}
	if im.users, err = im.importUsers(ctx, pages); err != nil {
		return out, err
	}

	for _, page := range pages {
		article, err := d.ArticleByName(ctx, siteID, page.Name)
		if errors.Is(err, db.ErrNotFound) {
			continue
		}
		if err != nil {
			return out, err
		}

		state, newer, err := stateOf(ctx, d, article.ID, page)
		if err != nil {
			return out, err
		}
		switch state {
		case stateEdited:
			out.Edited = append(out.Edited, page.Name)
		case stateNewer:
			n, err := im.updatePage(ctx, page, article.ID, newer)
			if err != nil {
				return out, err
			}
			out.Pages++
			out.Revisions += n
		}
	}
	return out, nil
}

func (im *importer) updatePage(ctx context.Context, page Page, articleID int64, newer []Revision) (int, error) {
	sources, err := im.archive.Sources(im.slug, page)
	if err != nil {
		return 0, err
	}
	u := db.ImportUpdate{
		Title:     page.Title,
		Locked:    page.Locked,
		UpdatedAt: time.Unix(page.Revisions[0].Stamp, 0).UTC(),
	}
	for i := len(newer) - 1; i >= 0; i-- {
		rev := newer[i]
		one := db.ImportRevision{
			Number:  rev.Number,
			UserID:  localUser(im.users, rev.Author),
			Comment: rev.Comment,
			At:      time.Unix(rev.Stamp, 0).UTC(),
			IsNew:   rev.IsNew(),
		}
		if source, ok := sources[rev.Number]; ok {
			one.Source = &source
			u.Indexed = source
		}
		u.Revisions = append(u.Revisions, one)
	}
	if im.opts.Tags {
		ids, err := im.db.EnsureTags(ctx, im.siteID, page.Tags)
		if err != nil {
			return 0, err
		}
		u.ReplaceTags = true
		u.TagIDs = ids
	}
	return len(u.Revisions), im.db.UpdateImportedArticle(ctx, im.siteID, articleID, u)
}
