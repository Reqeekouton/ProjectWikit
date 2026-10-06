package db

import (
	"testing"
	"time"
)

func TestAdminUserRowMessagesAt(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	earlier := now.Add(-time.Hour)
	cases := []struct {
		flag  bool
		until *time.Time
		want  bool
	}{
		{true, nil, true},
		{false, nil, false},
		{false, &later, false},
		{false, &earlier, true},
		{true, &later, false},
	}
	for _, c := range cases {
		u := AdminUserRow{CanSendDM: c.flag, DMUntil: c.until}
		if got := u.MessagesAt(now); got != c.want {
			t.Errorf("MessagesAt(flag=%v, until=%v) = %v, want %v", c.flag, c.until, got, c.want)
		}
	}
}

func TestUserMessagesAt(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	later := now.Add(time.Hour)
	u := User{CanSendDirectMessages: true, DirectMessagesUntil: &later}
	if u.MessagesAt(now) {
		t.Errorf("MessagesAt(until=%v) = true, want false", later)
	}
}
