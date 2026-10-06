package db

import (
	"context"
	"testing"
	"time"
)

func TestReserveClaimCodeHoldsOffASecondSend(t *testing.T) {
	d := writeTestDB(t)
	ctx := context.Background()
	id := scratchUser(t, d, "probe-claim-reserve")
	now := time.Now().UTC().Truncate(time.Second)

	ok, err := d.ReserveClaimCode(ctx, id, "a", now, now.Add(time.Hour), now.Add(-time.Minute))
	if err != nil || !ok {
		t.Fatalf("ReserveClaimCode(first) = %v, %v, want true, nil", ok, err)
	}
	ok, err = d.ReserveClaimCode(ctx, id, "b", now, now.Add(time.Hour), now.Add(-time.Minute))
	if err != nil || ok {
		t.Errorf("ReserveClaimCode(too soon) = %v, %v, want false, nil", ok, err)
	}
	later := now.Add(2 * time.Minute)
	ok, err = d.ReserveClaimCode(ctx, id, "c", later, later.Add(time.Hour), later.Add(-time.Minute))
	if err != nil || !ok {
		t.Errorf("ReserveClaimCode(later) = %v, %v, want true, nil", ok, err)
	}
	if got, err := d.UseClaimCode(ctx, id, "c", later, 5); err != nil || got != ClaimMatched {
		t.Errorf("UseClaimCode(c) = %v, %v, want ClaimMatched, nil", got, err)
	}
}

func TestDropClaimCodeKeepsANewerCode(t *testing.T) {
	d := writeTestDB(t)
	ctx := context.Background()
	id := scratchUser(t, d, "probe-claim-drop")
	now := time.Now().UTC()

	if _, err := d.ReserveClaimCode(ctx, id, "new", now, now.Add(time.Hour), now.Add(-time.Minute)); err != nil {
		t.Fatalf("ReserveClaimCode() err = %v, want nil", err)
	}
	if err := d.DropClaimCode(ctx, id, "old"); err != nil {
		t.Fatalf("DropClaimCode() err = %v, want nil", err)
	}
	if got, err := d.UseClaimCode(ctx, id, "new", now, 5); err != nil || got != ClaimMatched {
		t.Errorf("UseClaimCode(new) = %v, %v, want ClaimMatched, nil", got, err)
	}
}

func TestUseClaimCodeOutcomes(t *testing.T) {
	d := writeTestDB(t)
	ctx := context.Background()
	id := scratchUser(t, d, "probe-claim-use")
	now := time.Now().UTC()
	reserve := func() {
		t.Helper()
		if err := d.DropClaimCode(ctx, id, "right"); err != nil {
			t.Fatalf("DropClaimCode() err = %v, want nil", err)
		}
		if _, err := d.ReserveClaimCode(ctx, id, "right", now, now.Add(time.Hour), now); err != nil {
			t.Fatalf("ReserveClaimCode() err = %v, want nil", err)
		}
	}

	if got, _ := d.UseClaimCode(ctx, id, "right", now, 2); got != ClaimMissing {
		t.Errorf("UseClaimCode(nothing sent) = %v, want ClaimMissing", got)
	}

	reserve()
	for i := 0; i < 2; i++ {
		if got, _ := d.UseClaimCode(ctx, id, "wrong", now, 2); got != ClaimWrong {
			t.Errorf("UseClaimCode(wrong #%d) = %v, want ClaimWrong", i, got)
		}
	}
	if got, _ := d.UseClaimCode(ctx, id, "right", now, 2); got != ClaimLocked {
		t.Errorf("UseClaimCode(after two misses) = %v, want ClaimLocked", got)
	}

	reserve()
	if got, _ := d.UseClaimCode(ctx, id, "right", now.Add(2*time.Hour), 2); got != ClaimExpired {
		t.Errorf("UseClaimCode(late) = %v, want ClaimExpired", got)
	}
	if got, _ := d.UseClaimCode(ctx, id, "right", now, 2); got != ClaimMissing {
		t.Errorf("UseClaimCode(after expiry) = %v, want ClaimMissing", got)
	}
}
