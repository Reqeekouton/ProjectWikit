package db

import (
	"context"
	"slices"
	"testing"
)

func scratchWikidotUser(t *testing.T, d *DB, name string) int64 {
	t.Helper()
	id := scratchUser(t, d, name)
	if _, err := d.pool.Exec(context.Background(),
		`UPDATE web_user SET type = 'wikidot', wikidot_username = username WHERE id = $1`, id); err != nil {
		t.Fatalf("mark scratch user imported err = %v, want nil", err)
	}
	return id
}

func unreferencedIDs(t *testing.T, d *DB) []int64 {
	t.Helper()
	users, err := d.UnreferencedWikidotUsers(context.Background())
	if err != nil {
		t.Fatalf("UnreferencedWikidotUsers() err = %v, want nil", err)
	}
	ids := make([]int64, len(users))
	for i, u := range users {
		ids[i] = u.ID
	}
	return ids
}

func TestUnreferencedWikidotUsersListsAnUnusedImport(t *testing.T) {
	d := writeTestDB(t)
	id := scratchWikidotUser(t, d, "probe-prune-unused")

	if got := unreferencedIDs(t, d); !slices.Contains(got, id) {
		t.Errorf("UnreferencedWikidotUsers() lacks %d, want it listed", id)
	}
}

func TestUnreferencedWikidotUsersSkipsARoleHolder(t *testing.T) {
	d := writeTestDB(t)
	siteID := scratchSite(t, d)
	id := scratchWikidotUser(t, d, "probe-prune-role")
	role := scratchRole(t, d, siteID)
	grant(t, d, id, role)
	t.Cleanup(func() {
		if _, err := d.pool.Exec(context.Background(), `DELETE FROM web_user_roles WHERE user_id = $1`, id); err != nil {
			t.Errorf("clean up roles err = %v, want nil", err)
		}
	})

	if got := unreferencedIDs(t, d); slices.Contains(got, id) {
		t.Errorf("UnreferencedWikidotUsers() lists %d, want it skipped", id)
	}
}

func TestUnreferencedWikidotUsersSkipsALocalAccount(t *testing.T) {
	d := writeTestDB(t)
	id := scratchUser(t, d, "probe-prune-local")

	if got := unreferencedIDs(t, d); slices.Contains(got, id) {
		t.Errorf("UnreferencedWikidotUsers() lists %d, want it skipped", id)
	}
}

func TestDeleteUnreferencedWikidotUsersKeepsAReferencedOne(t *testing.T) {
	d := writeTestDB(t)
	siteID := scratchSite(t, d)
	unused := scratchWikidotUser(t, d, "probe-prune-gone")
	used := scratchWikidotUser(t, d, "probe-prune-kept")
	role := scratchRole(t, d, siteID)
	grant(t, d, used, role)
	t.Cleanup(func() {
		if _, err := d.pool.Exec(context.Background(), `DELETE FROM web_user_roles WHERE user_id = $1`, used); err != nil {
			t.Errorf("clean up roles err = %v, want nil", err)
		}
	})

	deleted, err := d.DeleteUnreferencedWikidotUsers(context.Background(), []int64{unused, used})
	if err != nil {
		t.Fatalf("DeleteUnreferencedWikidotUsers() err = %v, want nil", err)
	}
	if deleted != 1 {
		t.Errorf("DeleteUnreferencedWikidotUsers() = %d, want 1", deleted)
	}
	if _, err := d.UserByID(context.Background(), used); err != nil {
		t.Errorf("UserByID(%d) err = %v, want nil", used, err)
	}
}
