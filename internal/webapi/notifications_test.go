package webapi

import (
	"testing"
	"time"

	"github.com/WikitTeam/ProjectWikit/internal/db"
	"github.com/WikitTeam/ProjectWikit/internal/wikijson"
)

func siteField(t *testing.T, one db.Notification, current *db.Site) (wikijson.Object, bool) {
	t.Helper()
	out, err := notificationJSON(one, current, "https")
	if err != nil {
		t.Fatalf("notificationJSON() err = %v, want nil", err)
	}
	for _, f := range out {
		if f.Key == "site" {
			site, ok := f.Value.(wikijson.Object)
			if !ok {
				t.Fatalf("notificationJSON() site = %T, want wikijson.Object", f.Value)
			}
			return site, true
		}
	}
	return nil, false
}

func likeFrom(siteID int64) db.Notification {
	return db.Notification{
		ID: 1, Type: db.NotifyPostLike, Meta: []byte(`{}`), CreatedAt: time.Unix(0, 0),
		SiteID: siteID, SiteTitle: "Other", SiteDomain: "other.example",
	}
}

func TestNotificationJSONNamesAnotherSite(t *testing.T) {
	site, ok := siteField(t, likeFrom(2), &db.Site{ID: 1})
	if !ok {
		t.Fatal("notificationJSON() has no site, want one")
	}
	want := wikijson.Object{
		{Key: "title", Value: "Other"},
		{Key: "url", Value: "https://other.example"},
	}
	if len(site) != len(want) {
		t.Fatalf("notificationJSON() site = %v, want %v", site, want)
	}
	for i := range want {
		if site[i] != want[i] {
			t.Errorf("notificationJSON() site[%d] = %v, want %v", i, site[i], want[i])
		}
	}
}

func TestNotificationJSONLeavesTheCurrentSiteOut(t *testing.T) {
	if site, ok := siteField(t, likeFrom(1), &db.Site{ID: 1}); ok {
		t.Errorf("notificationJSON() site = %v, want none", site)
	}
}

func TestNotificationJSONLeavesANotificationWithoutASiteAlone(t *testing.T) {
	if site, ok := siteField(t, likeFrom(0), &db.Site{ID: 1}); ok {
		t.Errorf("notificationJSON() site = %v, want none", site)
	}
}
