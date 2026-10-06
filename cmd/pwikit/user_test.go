package main

import (
	"testing"

	"github.com/WikitTeam/ProjectWikit/internal/db"
)

func TestSplitMentioned(t *testing.T) {
	candidates := []db.UnusedUser{
		{ID: 1, WikidotUsername: "Some User"},
		{ID: 2, WikidotUsername: "other"},
		{ID: 3, WikidotUsername: "shown", DisplayName: "Shown Name"},
		{ID: 4, WikidotUsername: "Prefixed"},
		{ID: 5, WikidotUsername: "nobody"},
	}
	mentions := []string{"some-user", " Shown Name ", "wd:prefixed", "external:nobody"}

	unused, named := splitMentioned(candidates, mentions)
	if named != 3 {
		t.Errorf("splitMentioned() named = %d, want 3", named)
	}
	var ids []int64
	for _, u := range unused {
		ids = append(ids, u.ID)
	}
	if len(ids) != 2 || ids[0] != 2 || ids[1] != 5 {
		t.Errorf("splitMentioned() unused = %v, want [2 5]", ids)
	}
}
