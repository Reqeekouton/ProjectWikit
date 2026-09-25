package db

import (
	"context"
	"testing"
	"time"
)

func ensureOne(t *testing.T, d *DB, u ImportUser) int64 {
	t.Helper()
	got, err := d.EnsureWikidotUsers(context.Background(), []ImportUser{u}, time.Now().UTC())
	if err != nil {
		t.Fatalf("EnsureWikidotUsers() err = %v, want nil", err)
	}
	local, ok := got[u.WikidotID]
	if !ok {
		t.Fatalf("EnsureWikidotUsers() = %v, want an id for %d", got, u.WikidotID)
	}
	t.Cleanup(func() {
		if _, err := d.pool.Exec(context.Background(), `DELETE FROM web_user WHERE id = $1`, local); err != nil {
			t.Errorf("clean up imported user err = %v, want nil", err)
		}
	})
	return local
}

func wikidotNames(t *testing.T, d *DB, id int64) (string, string) {
	t.Helper()
	var name, display string
	err := d.pool.QueryRow(context.Background(),
		`SELECT coalesce(wikidot_username, ''), coalesce(display_name, '') FROM web_user WHERE id = $1`, id).Scan(&name, &display)
	if err != nil {
		t.Fatalf("read imported user err = %v, want nil", err)
	}
	return name, display
}

func TestEnsureWikidotUsersNamesADeletedUser(t *testing.T) {
	d := writeTestDB(t)
	wikidotID := 3_000_000_000 + time.Now().UnixNano()%1_000_000_000
	local := ensureOne(t, d, ImportUser{WikidotID: wikidotID, DisplayName: "gone"})

	name, display := wikidotNames(t, d, local)
	if name != DeletedWikidotName(wikidotID) {
		t.Errorf("EnsureWikidotUsers() wikidot_username = %q, want %q", name, DeletedWikidotName(wikidotID))
	}
	if display != "gone" {
		t.Errorf("EnsureWikidotUsers() display_name = %q, want %q", display, "gone")
	}
}

func TestEnsureWikidotUsersNamesAPlaceholderOnceKnown(t *testing.T) {
	d := writeTestDB(t)
	wikidotID := 3_000_000_000 + time.Now().UnixNano()%1_000_000_000
	local := ensureOne(t, d, ImportUser{WikidotID: wikidotID, DisplayName: "gone"})

	again := ensureOne(t, d, ImportUser{WikidotID: wikidotID, Username: "Probe-Known", DisplayName: "Probe Known"})
	if again != local {
		t.Fatalf("EnsureWikidotUsers() = #%d, want #%d", again, local)
	}
	name, display := wikidotNames(t, d, local)
	if name != "Probe-Known" {
		t.Errorf("EnsureWikidotUsers() wikidot_username = %q, want %q", name, "Probe-Known")
	}
	if display != "Probe Known" {
		t.Errorf("EnsureWikidotUsers() display_name = %q, want %q", display, "Probe Known")
	}
}

func TestEnsureWikidotUsersLeavesANamedAccountAlone(t *testing.T) {
	d := writeTestDB(t)
	wikidotID := 3_000_000_000 + time.Now().UnixNano()%1_000_000_000
	local := ensureOne(t, d, ImportUser{WikidotID: wikidotID, Username: "Probe-First", DisplayName: "First"})

	ensureOne(t, d, ImportUser{WikidotID: wikidotID, Username: "Probe-Second", DisplayName: "Second"})
	if name, _ := wikidotNames(t, d, local); name != "Probe-First" {
		t.Errorf("EnsureWikidotUsers() wikidot_username = %q, want %q", name, "Probe-First")
	}
}
