package archive

import (
	"testing"
	"time"

	"github.com/WikitTeam/ProjectWikit/internal/db"
)

func ptr(v int64) *int64 { return &v }

func TestMatchPost(t *testing.T) {
	at := time.Unix(1600000000, 0).UTC()
	stored := []db.PostKey{
		{ID: 1, At: at, AuthorID: ptr(7)},
		{ID: 2, At: at, AuthorID: nil},
		{ID: 3, At: at.Add(time.Minute), AuthorID: ptr(8)},
	}
	cases := []struct {
		name string
		used []bool
		post db.ImportPost
		want int
	}{
		{"same time and author", []bool{false, false, false}, db.ImportPost{CreatedAt: at, AuthorID: ptr(7)}, 0},
		{"stored author missing", []bool{true, false, false}, db.ImportPost{CreatedAt: at, AuthorID: ptr(9)}, 1},
		{"other author", []bool{false, true, false}, db.ImportPost{CreatedAt: at, AuthorID: ptr(9)}, -1},
		{"other time", []bool{false, false, false}, db.ImportPost{CreatedAt: at.Add(time.Hour), AuthorID: ptr(7)}, -1},
		{"already used", []bool{false, false, true}, db.ImportPost{CreatedAt: at.Add(time.Minute), AuthorID: ptr(8)}, -1},
	}
	for _, c := range cases {
		if got := matchPost(stored, c.used, c.post); got != c.want {
			t.Errorf("matchPost(%s) = %d, want %d", c.name, got, c.want)
		}
	}
}
