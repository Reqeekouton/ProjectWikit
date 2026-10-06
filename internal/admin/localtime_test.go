package admin

import (
	"testing"
	"time"
)

func TestWhenSplit(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 42, 7, 0, time.FixedZone("JST", 9*3600))
	want := `<time datetime="2026-10-02T03:42:07Z" data-local-time="split">2026-10-02<br><strong>03:42:07 UTC</strong></time>`
	split := funcs()["whenSplit"].(func(any) string)
	if got := split(at); got != want {
		t.Errorf("whenSplit(%v) = %q, want %q", at, got, want)
	}
	if got := split(&at); got != want {
		t.Errorf("whenSplit(&%v) = %q, want %q", at, got, want)
	}
}

func TestWhen(t *testing.T) {
	at := time.Date(2026, 10, 2, 12, 42, 7, 0, time.FixedZone("JST", 9*3600))
	want := `<time datetime="2026-10-02T03:42:07Z" data-local-time>2026-10-02 03:42 UTC</time>`
	when := funcs()["when"].(func(any) string)
	if got := when(at); got != want {
		t.Errorf("when(%v) = %q, want %q", at, got, want)
	}
}

func TestWhenEmpty(t *testing.T) {
	when := funcs()["when"].(func(any) string)
	var missing *time.Time
	if got := when(missing); got != "" {
		t.Errorf("when(nil) = %q, want %q", got, "")
	}
	if got := when(time.Time{}); got != "" {
		t.Errorf("when(time.Time{}) = %q, want %q", got, "")
	}
}
