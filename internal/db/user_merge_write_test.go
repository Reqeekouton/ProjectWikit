package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

func mergePair(t *testing.T, d *DB) (siteID, from, into int64) {
	t.Helper()
	siteID = scratchSite(t, d)
	from = scratchUser(t, d, "probe-merge-from")
	into = scratchUser(t, d, "probe-merge-into")
	t.Cleanup(func() {
		if _, err := d.pool.Exec(context.Background(),
			`DELETE FROM web_user_roles WHERE user_id = ANY($1)`, []int64{from, into}); err != nil {
			t.Errorf("clean up roles err = %v, want nil", err)
		}
	})
	return siteID, from, into
}

func rolesOf(t *testing.T, d *DB, user int64) []int64 {
	t.Helper()
	rows, err := d.pool.Query(context.Background(),
		`SELECT role_id FROM web_user_roles WHERE user_id = $1 ORDER BY role_id`, user)
	if err != nil {
		t.Fatalf("read roles err = %v, want nil", err)
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatalf("scan role err = %v, want nil", err)
		}
		out = append(out, id)
	}
	return out
}

func grant(t *testing.T, d *DB, user, role int64) {
	t.Helper()
	if _, err := d.pool.Exec(context.Background(),
		`INSERT INTO web_user_roles (user_id, role_id) VALUES ($1, $2)`, user, role); err != nil {
		t.Fatalf("grant role err = %v, want nil", err)
	}
}

func wikidotIdentity(t *testing.T, d *DB, user int64, name string, id int64) {
	t.Helper()
	if _, err := d.pool.Exec(context.Background(),
		`UPDATE web_user SET wikidot_username = $2, wikidot_user_id = $3 WHERE id = $1`, user, name, id); err != nil {
		t.Fatalf("set wikidot identity err = %v, want nil", err)
	}
}

func TestMergeUsersMovesRoles(t *testing.T) {
	d := writeTestDB(t)
	siteID, from, into := mergePair(t, d)
	role := scratchRole(t, d, siteID)
	grant(t, d, from, role)

	if _, err := d.MergeUsers(context.Background(), from, into); err != nil {
		t.Fatalf("MergeUsers() err = %v, want nil", err)
	}
	if got := rolesOf(t, d, into); len(got) != 1 || got[0] != role {
		t.Errorf("MergeUsers() roles of into = %v, want [%d]", got, role)
	}
}

func TestMergeUsersKeepsOneOfATwiceHeldRole(t *testing.T) {
	d := writeTestDB(t)
	siteID, from, into := mergePair(t, d)
	role := scratchRole(t, d, siteID)
	grant(t, d, from, role)
	grant(t, d, into, role)

	if _, err := d.MergeUsers(context.Background(), from, into); err != nil {
		t.Fatalf("MergeUsers() err = %v, want nil", err)
	}
	if got := rolesOf(t, d, into); len(got) != 1 || got[0] != role {
		t.Errorf("MergeUsers() roles of into = %v, want [%d]", got, role)
	}
}

func TestMergeUsersDeletesTheSource(t *testing.T) {
	d := writeTestDB(t)
	_, from, into := mergePair(t, d)

	if _, err := d.MergeUsers(context.Background(), from, into); err != nil {
		t.Fatalf("MergeUsers() err = %v, want nil", err)
	}
	if _, err := d.UserByID(context.Background(), from); !errors.Is(err, ErrNotFound) {
		t.Errorf("UserByID(from) err = %v, want ErrNotFound", err)
	}
}

func TestMergeUsersCarriesTheWikidotIdentity(t *testing.T) {
	d := writeTestDB(t)
	_, from, into := mergePair(t, d)
	wikidotID := time.Now().UnixNano()
	wikidotIdentity(t, d, from, "Probe-Merge", wikidotID)

	if _, err := d.MergeUsers(context.Background(), from, into); err != nil {
		t.Fatalf("MergeUsers() err = %v, want nil", err)
	}
	got, err := d.UserByWikidotAlias(context.Background(), "probe-merge")
	if err != nil {
		t.Fatalf("UserByWikidotAlias() err = %v, want nil", err)
	}
	if got.ID != into {
		t.Errorf("UserByWikidotAlias() = #%d, want #%d", got.ID, into)
	}
}

func TestMergeUsersRefusesTwoWikidotIdentities(t *testing.T) {
	d := writeTestDB(t)
	_, from, into := mergePair(t, d)
	stamp := time.Now().UnixNano()
	wikidotIdentity(t, d, from, "Probe-Merge-A", stamp)
	wikidotIdentity(t, d, into, "Probe-Merge-B", stamp+1)

	if _, err := d.MergeUsers(context.Background(), from, into); !errors.Is(err, ErrMergeTwoWikidot) {
		t.Errorf("MergeUsers() err = %v, want ErrMergeTwoWikidot", err)
	}
	if _, err := d.UserByID(context.Background(), from); err != nil {
		t.Errorf("UserByID(from) err = %v, want nil", err)
	}
}

func TestMergeUsersRefusesTheSameAccount(t *testing.T) {
	d := writeTestDB(t)
	_, from, _ := mergePair(t, d)

	if _, err := d.MergeUsers(context.Background(), from, from); !errors.Is(err, ErrMergeSame) {
		t.Errorf("MergeUsers() err = %v, want ErrMergeSame", err)
	}
}
