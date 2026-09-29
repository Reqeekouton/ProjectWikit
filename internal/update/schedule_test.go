package update

import (
	"errors"
	"testing"
	"time"

	"github.com/WikitTeam/ProjectWikit/internal/db"
)

func TestNewer(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"v1.0.1", "v1.0.0", true},
		{"v1.1.0", "v1.0.9", true},
		{"v2.0.0", "v1.99.99", true},
		{"v1.0.0", "v1.0.0", false},
		{"v1.0.0", "v1.0.1", false},
		{"v1.0.0", "v1.0.0-rc.1", true},
		{"v1.0.0-rc.2", "v1.0.0-rc.1", true},
		{"v1.0.0-rc.10", "v1.0.0-rc.9", true},
		{"v1.0.0-rc.1", "v1.0.0", false},
		{"v1.0.1", "cf4ec6467c79", false},
		{"nonsense", "v1.0.0", false},
	}
	for _, tt := range cases {
		if got := Newer(tt.candidate, tt.current); got != tt.want {
			t.Errorf("Newer(%q, %q) = %t, want %t", tt.candidate, tt.current, got, tt.want)
		}
	}
}

func TestParseWindow(t *testing.T) {
	for _, bad := range []string{"", "03:00", "3-5", "25:00-26:00", "03:00-03:05"} {
		if _, err := ParseWindow(bad); err == nil {
			t.Errorf("ParseWindow(%q) err = nil, want an error", bad)
		}
	}
	w, err := ParseWindow("23:30-01:00")
	if err != nil {
		t.Fatalf("ParseWindow(across midnight) err = %v, want nil", err)
	}
	if w.length() != 90*time.Minute {
		t.Errorf("ParseWindow(23:30-01:00).length() = %s, want 1h30m", w.length())
	}
}

func TestWindowNext(t *testing.T) {
	zone := time.FixedZone("server", 9*3600)
	cases := []struct {
		name   string
		window string
		at     time.Time
		latest bool
		want   time.Time
	}{
		{"before the window", DefaultWindow, time.Date(2026, 9, 13, 2, 0, 0, 0, zone), false, time.Date(2026, 9, 13, 3, 0, 0, 0, zone)},
		{"just before the window", DefaultWindow, time.Date(2026, 9, 13, 2, 55, 0, 0, zone), false, time.Date(2026, 9, 13, 3, 5, 0, 0, zone)},
		{"inside the window", DefaultWindow, time.Date(2026, 9, 13, 4, 0, 0, 0, zone), false, time.Date(2026, 9, 13, 4, 10, 0, 0, zone)},
		{"too late for the warning", DefaultWindow, time.Date(2026, 9, 13, 4, 51, 0, 0, zone), false, time.Date(2026, 9, 14, 3, 0, 0, 0, zone)},
		{"afternoon", DefaultWindow, time.Date(2026, 9, 13, 15, 0, 0, 0, zone), false, time.Date(2026, 9, 14, 3, 0, 0, 0, zone)},
		{"afternoon latest", DefaultWindow, time.Date(2026, 9, 13, 15, 0, 0, 0, zone), true, time.Date(2026, 9, 14, 4, 59, 59, 0, zone)},
		{"across midnight before it", "23:00-01:00", time.Date(2026, 9, 13, 22, 0, 0, 0, zone), false, time.Date(2026, 9, 13, 23, 0, 0, 0, zone)},
		{"across midnight after it", "23:00-01:00", time.Date(2026, 9, 14, 0, 20, 0, 0, zone), false, time.Date(2026, 9, 14, 0, 30, 0, 0, zone)},
		{"across midnight too late", "23:00-01:00", time.Date(2026, 9, 14, 0, 55, 0, 0, zone), false, time.Date(2026, 9, 14, 23, 0, 0, 0, zone)},
	}
	for _, c := range cases {
		w, err := ParseWindow(c.window)
		if err != nil {
			t.Fatalf("ParseWindow(%q) err = %v, want nil", c.window, err)
		}
		pickFirst(t)
		if c.latest {
			pick = func(n time.Duration) time.Duration { return n - time.Second }
		}
		if got := w.Next(c.at); !got.Equal(c.want) {
			t.Errorf("Window(%s).Next(%s) = %s, want %s", c.window, c.name, got, c.want)
		}
	}
}

func pickFirst(t *testing.T) {
	saved := pick
	pick = func(time.Duration) time.Duration { return 0 }
	t.Cleanup(func() { pick = saved })
}

func eligibleState(now time.Time) db.UpdateState {
	published := now.Add(-48 * time.Hour)
	return db.UpdateState{LatestVersion: "v1.1.0", LatestPublishedAt: &published, LatestPostgres: "18.6.0"}
}

func defaultSettings() Settings {
	w, _ := ParseWindow(DefaultWindow)
	return Settings{Auto: true, PublicBanner: true, Check: true, Window: w, MinAge: DefaultMinAge}
}

func TestEligible(t *testing.T) {
	now := time.Date(2026, 9, 13, 3, 0, 0, 0, time.UTC)
	facts := Facts{Current: "v1.0.0", BundledPostgres: "18.6.0", Now: now}
	later := now.Add(time.Hour)
	fresh := now.Add(-time.Hour)

	cases := []struct {
		name   string
		change func(*db.UpdateState, *Settings, *Facts)
		want   string
	}{
		{"eligible", func(*db.UpdateState, *Settings, *Facts) {}, "v1.1.0"},
		{"auto off", func(_ *db.UpdateState, s *Settings, _ *Facts) { s.Auto = false }, ""},
		{"check off", func(_ *db.UpdateState, s *Settings, _ *Facts) { s.Check = false }, ""},
		{"container", func(_ *db.UpdateState, _ *Settings, f *Facts) { f.Container = true }, ""},
		{"development build", func(_ *db.UpdateState, _ *Settings, f *Facts) { f.Current = "cf4ec6467c79" }, ""},
		{"already newest", func(_ *db.UpdateState, _ *Settings, f *Facts) { f.Current = "v1.1.0" }, ""},
		{"pinned", func(st *db.UpdateState, _ *Settings, _ *Facts) { st.PinnedVersion = "v1.0.0" }, ""},
		{"skipped", func(st *db.UpdateState, _ *Settings, _ *Facts) { st.SkippedVersion = "v1.1.0" }, ""},
		{"skipped an older one", func(st *db.UpdateState, _ *Settings, _ *Facts) { st.SkippedVersion = "v1.0.5" }, "v1.1.0"},
		{"failed before", func(st *db.UpdateState, _ *Settings, _ *Facts) { st.FailedVersions = []string{"v1.1.0"} }, ""},
		{"postponed", func(st *db.UpdateState, _ *Settings, _ *Facts) { st.PostponedUntil = &later }, ""},
		{"fresh without an age", func(st *db.UpdateState, _ *Settings, _ *Facts) { st.LatestPublishedAt = &fresh }, "v1.1.0"},
		{"too fresh", func(st *db.UpdateState, s *Settings, _ *Facts) {
			st.LatestPublishedAt = &fresh
			s.MinAge = 12 * time.Hour
		}, ""},
		{"too fresh for a shorter age", func(st *db.UpdateState, s *Settings, _ *Facts) {
			st.LatestPublishedAt = &fresh
			s.MinAge = 30 * time.Minute
		}, "v1.1.0"},
		{"new PostgreSQL major", func(st *db.UpdateState, _ *Settings, _ *Facts) { st.LatestPostgres = "19.1.0" }, ""},
		{"own PostgreSQL ignores the major", func(st *db.UpdateState, _ *Settings, f *Facts) {
			st.LatestPostgres = "19.1.0"
			f.BundledPostgres = ""
		}, "v1.1.0"},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			st, s, f := eligibleState(now), defaultSettings(), facts
			tt.change(&st, &s, &f)
			got, reason := Eligible(st, s, f)
			if got != tt.want {
				t.Errorf("Eligible() = %q (%s), want %q", got, reason, tt.want)
			}
			if got == "" && reason.String() == "" {
				t.Error("Eligible() reason = empty, want one when nothing is eligible")
			}
		})
	}
}

func TestTickChecksHourlyAndSchedulesInTheNextWindow(t *testing.T) {
	pickFirst(t)
	zone := time.UTC
	s := defaultSettings()
	published := time.Date(2026, 9, 13, 11, 0, 0, 0, zone)
	manifest := Manifest{Version: "v1.1.0", PublishedAt: published.Format(time.RFC3339), Postgres: "18.6.0"}
	fetches := 0
	fetch := func() (Manifest, error) { fetches++; return manifest, nil }
	facts := func(now time.Time) Facts { return Facts{Current: "v1.0.0", BundledPostgres: "18.6.0", Now: now} }

	var st db.UpdateState
	noon := time.Date(2026, 9, 13, 12, 0, 0, 0, zone)
	if got := Tick(&st, s, facts(noon), fetch); got != "" || fetches != 1 {
		t.Fatalf("first Tick() = %q with %d fetches, want one fetch and nothing installed", got, fetches)
	}
	if st.LatestVersion != "v1.1.0" {
		t.Errorf("LatestVersion after the first Tick() = %q, want v1.1.0", st.LatestVersion)
	}
	if st.ScheduledVersion != "v1.1.0" || st.ScheduledAt == nil || st.ScheduledByHand {
		t.Fatalf("first Tick() left scheduled=%q at=%v byHand=%v, want v1.1.0 scheduled automatically", st.ScheduledVersion, st.ScheduledAt, st.ScheduledByHand)
	}
	if want := time.Date(2026, 9, 14, 3, 0, 0, 0, zone); !st.ScheduledAt.Equal(want) {
		t.Errorf("ScheduledAt = %s, want %s", st.ScheduledAt, want)
	}
	if _, ok := Banner(st, s, noon); ok {
		t.Errorf("Banner() hours before the update = shown, want hidden")
	}
	if got := Tick(&st, s, facts(noon.Add(50*time.Minute)), fetch); got != "" || fetches != 1 {
		t.Errorf("Tick() within the hour = %q with %d fetches, want no fetch", got, fetches)
	}
	if Tick(&st, s, facts(noon.Add(CheckEvery)), fetch); fetches != 2 {
		t.Errorf("fetches after an hour = %d, want 2", fetches)
	}
	if at, ok := Banner(st, s, st.ScheduledAt.Add(-AnnounceAhead+time.Minute)); !ok || !at.Equal(*st.ScheduledAt) {
		t.Errorf("Banner() = %s, %t, want the scheduled time shown", at, ok)
	}
	if got := Tick(&st, s, facts(st.ScheduledAt.Add(-time.Second)), fetch); got != "" {
		t.Errorf("Tick() a second early = %q, want nothing", got)
	}
	if got := Tick(&st, s, facts(*st.ScheduledAt), fetch); got != "v1.1.0" {
		t.Errorf("Tick() when due = %q, want v1.1.0", got)
	}
}

func TestTickWaitsForAReleaseToAge(t *testing.T) {
	s := defaultSettings()
	s.MinAge = 12 * time.Hour
	open := time.Date(2026, 9, 13, 3, 10, 0, 0, time.UTC)
	fresh := open.Add(-time.Hour)
	st := db.UpdateState{LatestVersion: "v1.1.0", LatestPublishedAt: &fresh}
	next := open.Add(time.Hour)
	st.NextCheckAt = &next
	fetch := func() (Manifest, error) { return Manifest{}, errors.New("offline") }
	if got := Tick(&st, s, Facts{Current: "v1.0.0", Now: open}, fetch); got != "" || st.ScheduledVersion != "" {
		t.Errorf("Tick() on a release an hour old = %q, scheduled %q, want nothing", got, st.ScheduledVersion)
	}
}

func TestStartNowIgnoresTheWindowAndTheAge(t *testing.T) {
	s := defaultSettings()
	s.Auto = false
	noon := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	fresh := noon.Add(-time.Hour)
	st := db.UpdateState{LatestVersion: "v1.1.0", LatestPublishedAt: &fresh, SkippedVersion: "v1.1.0"}
	next := noon.Add(time.Hour)
	st.NextCheckAt = &next
	facts := func(at time.Time) Facts { return Facts{Current: "v1.0.0", Now: at} }
	fetch := func() (Manifest, error) { return Manifest{}, errors.New("offline") }

	if !StartNow(&st, facts(noon)) {
		t.Fatalf("StartNow() = false, want true")
	}
	if want := noon.Add(AnnounceAhead); st.ScheduledAt == nil || !st.ScheduledAt.Equal(want) || !st.ScheduledByHand {
		t.Fatalf("StartNow() left at=%v byHand=%v, want %s by hand", st.ScheduledAt, st.ScheduledByHand, want)
	}
	if _, ok := Banner(st, s, noon.Add(time.Minute)); !ok {
		t.Errorf("Banner() with automatic updates off = hidden, want the requested update shown")
	}
	if got := Notices(st, s, facts(noon)); len(got) != 1 || got[0].Kind != NoticeScheduled {
		t.Errorf("Notices() = %+v, want one scheduled notice", got)
	}
	if got := Notices(st, s, facts(noon)); len(got) == 1 && !got[0].ByHand {
		t.Errorf("Notices()[0].ByHand = false, want true")
	}
	if got := Tick(&st, s, facts(noon.Add(5*time.Minute)), fetch); got != "" || st.ScheduledVersion != "v1.1.0" {
		t.Errorf("Tick() before it is due = %q, scheduled %q, want it kept", got, st.ScheduledVersion)
	}
	if got := Tick(&st, s, facts(noon.Add(AnnounceAhead)), fetch); got != "v1.1.0" {
		t.Errorf("Tick() when due = %q, want v1.1.0", got)
	}
}

func TestStartNowRefusesWhatCannotBeInstalled(t *testing.T) {
	now := time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name  string
		st    db.UpdateState
		facts Facts
	}{
		{"nothing newer", db.UpdateState{LatestVersion: "v1.0.0"}, Facts{Current: "v1.0.0", Now: now}},
		{"nothing known", db.UpdateState{}, Facts{Current: "v1.0.0", Now: now}},
		{"a development build", db.UpdateState{LatestVersion: "v1.1.0"}, Facts{Current: "dev", Now: now}},
		{"a container", db.UpdateState{LatestVersion: "v1.1.0"}, Facts{Current: "v1.0.0", Container: true, Now: now}},
	}
	for _, c := range cases {
		st := c.st
		if StartNow(&st, c.facts) || st.ScheduledVersion != "" {
			t.Errorf("StartNow(%s) scheduled %q, want nothing", c.name, st.ScheduledVersion)
		}
	}
}

func TestTickDropsASkippedOrMissedSchedule(t *testing.T) {
	s := defaultSettings()
	now := time.Date(2026, 9, 13, 3, 30, 0, 0, time.UTC)
	fetch := func() (Manifest, error) { return Manifest{}, errors.New("offline") }
	facts := func(at time.Time) Facts { return Facts{Current: "v1.0.0", Now: at} }

	st := eligibleState(now)
	next := now.Add(24 * time.Hour)
	st.NextCheckAt = &next
	at := now.Add(10 * time.Minute)
	st.ScheduledVersion, st.ScheduledAt = "v1.1.0", &at

	skipped := st
	Skip(&skipped)
	if got := Tick(&skipped, s, facts(at), fetch); got != "" || skipped.ScheduledVersion != "" {
		t.Errorf("Tick() after Skip = %q, scheduled %q, want nothing", got, skipped.ScheduledVersion)
	}

	missed := st
	if got := Tick(&missed, s, facts(at.Add(MissedAfter+time.Minute)), fetch); got != "" || missed.ScheduledAt != nil {
		t.Errorf("Tick() long after the schedule = %q, at %v, want it dropped", got, missed.ScheduledAt)
	}
}

func TestNoticesAfterARollback(t *testing.T) {
	now := time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC)
	at := now.Add(-time.Hour)
	st := db.UpdateState{LastOutcome: OutcomeRolledBack, LastFrom: "v1.0.0", LastTo: "v1.1.0", LastAt: &at, LastError: "health check failed"}
	got := Notices(st, defaultSettings(), Facts{Current: "v1.0.0", Now: now})
	if len(got) != 1 || got[0].Kind != NoticeRolledBack || got[0].Error != "health check failed" {
		t.Errorf("Notices() = %+v, want one rolled-back notice with the error", got)
	}
}

func TestNewlyFound(t *testing.T) {
	cases := []struct {
		name, previous, latest, current string
		want                            bool
	}{
		{"first sight of a newer release", "v1.0.0", "v1.1.0", "v1.0.0", true},
		{"nothing known before", "", "v1.1.0", "v1.0.0", true},
		{"same release checked again", "v1.1.0", "v1.1.0", "v1.0.0", false},
		{"the release already runs", "v1.0.0", "v1.1.0", "v1.1.0", false},
		{"older than what runs", "", "v0.9.0", "v1.0.0", false},
		{"the check failed", "v1.0.0", "", "v1.0.0", false},
	}
	for _, c := range cases {
		got := NewlyFound(c.previous, db.UpdateState{LatestVersion: c.latest}, c.current)
		if got != c.want {
			t.Errorf("NewlyFound(%s) = %v, want %v", c.name, got, c.want)
		}
	}
}
