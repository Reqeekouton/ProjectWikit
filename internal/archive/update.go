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
