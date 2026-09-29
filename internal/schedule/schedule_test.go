// Schedule store and timing tests: daily, cron, once expiry, limits.
package schedule

import (
	"strings"
	"testing"
	"time"
)

func testNow() time.Time {
	return time.Date(2026, 9, 28, 12, 0, 0, 0, time.UTC)
}

func TestParseDailyTime(t *testing.T) {
	hour, min, err := ParseDailyTime("06:00")
	if err != nil || hour != 6 || min != 0 {
		t.Fatalf("06:00 = %d:%d, err=%v", hour, min, err)
	}
	hour, min, err = ParseDailyTime("6")
	if err != nil || hour != 6 || min != 0 {
		t.Fatalf("6 = %d:%d, err=%v", hour, min, err)
	}
	if _, _, err := ParseDailyTime("25:00"); err == nil {
		t.Fatalf("25:00 must fail")
	}
	if _, _, err := ParseDailyTime(""); err == nil {
		t.Fatalf("empty must fail")
	}
}

func TestNormalizeTimezone(t *testing.T) {
	if got := NormalizeTimezone(""); got != DefaultTimezone {
		t.Fatalf("empty = %q, want %q", got, DefaultTimezone)
	}
	if got := NormalizeTimezone("Asia/Jakarta"); got != "Asia/Jakarta" {
		t.Fatalf("jakarta = %q", got)
	}
	if got := NormalizeTimezone("Bogus/Zone"); got != DefaultTimezone {
		t.Fatalf("bogus must fall back, got %q", got)
	}
}

func TestDailyNextRunWIB(t *testing.T) {
	now := testNow()
	job := Job{Type: TypeDaily, DailyTime: "06:00", Timezone: "Asia/Jakarta", Enabled: true}
	next, ok := ComputeNext(job, now)
	if !ok {
		t.Fatalf("daily must have next run")
	}
	loc := ResolveLocation("Asia/Jakarta")
	inLoc := next.In(loc)
	if inLoc.Hour() != 6 || inLoc.Minute() != 0 {
		t.Fatalf("next daily = %v, want 06:00 WIB", inLoc)
	}
	if !next.After(now) {
		t.Fatalf("next must be after now")
	}
}

func TestDailyWeekdays(t *testing.T) {
	// Monday 2026-09-28 12:00 UTC = 19:00 WIB Monday.
	now := testNow()
	job := Job{Type: TypeDaily, DailyTime: "06:00", Weekdays: []string{"tue"}, Timezone: "Asia/Jakarta", Enabled: true}
	next, ok := ComputeNext(job, now)
	if !ok {
		t.Fatalf("weekday daily must have next run")
	}
	if next.In(ResolveLocation("Asia/Jakarta")).Weekday() != time.Tuesday {
		t.Fatalf("next must be Tuesday, got %v", next)
	}
	if _, err := normalizeWeekdayList([]string{"funday"}); err == nil {
		t.Fatalf("funday must fail")
	}
}

func TestOnceValidation(t *testing.T) {
	now := testNow()
	store := NewStore(t.TempDir())
	past := now.Add(-time.Hour)
	job := Job{Name: "past", Prompt: "do x", Type: TypeOnce, At: past}
	if _, err := store.Add(job, now); err == nil {
		t.Fatalf("past once must be rejected")
	}
	future := now.Add(time.Hour)
	saved, err := store.Add(Job{Name: "future", Prompt: "do x", Type: TypeOnce, At: future}, now)
	if err != nil {
		t.Fatalf("future once: %v", err)
	}
	if saved.NextRunAt.IsZero() {
		t.Fatalf("once must have next run")
	}
	// Due after time passes, then auto-deleted on fire.
	due, err := store.Due(now.Add(2 * time.Hour))
	if err != nil || len(due) != 1 {
		t.Fatalf("due = %v, err=%v", due, err)
	}
	_, deleted, err := store.MarkFired(saved.ID, now.Add(2*time.Hour))
	if err != nil || !deleted {
		t.Fatalf("once must delete after fire, deleted=%v err=%v", deleted, err)
	}
}

func TestEveryAndLimits(t *testing.T) {
	now := testNow()
	store := NewStore(t.TempDir())
	saved, err := store.Add(Job{Name: "tick", Prompt: "do x", Type: TypeEvery, EverySeconds: 3600, MaxRuns: 2}, now)
	if err != nil {
		t.Fatalf("every add: %v", err)
	}
	if saved.NextRunAt.IsZero() {
		t.Fatalf("every must have next run")
	}
	// First fire keeps the job.
	if _, deleted, err := store.MarkFired(saved.ID, now.Add(2*time.Hour)); err != nil || deleted {
		t.Fatalf("first fire must keep job, deleted=%v err=%v", deleted, err)
	}
	// Second fire hits max_runs and deletes.
	if _, deleted, err := store.MarkFired(saved.ID, now.Add(3*time.Hour)); err != nil || !deleted {
		t.Fatalf("second fire must delete at max_runs, deleted=%v err=%v", deleted, err)
	}
}

func TestDaysLimitEndsJob(t *testing.T) {
	now := testNow()
	end := now.Add(24 * time.Hour)
	job := Job{Type: TypeDaily, DailyTime: "06:00", Timezone: "Asia/Jakarta", Enabled: true, EndAt: &end}
	// Now + 2 days is past the end date: no future run.
	if _, ok := ComputeNext(job, now.Add(48*time.Hour)); ok {
		t.Fatalf("past end date must have no next run")
	}
}

func TestCronExpr(t *testing.T) {
	now := testNow()
	job := Job{Type: TypeCron, CronExpr: "0 6 * * *", Timezone: "Asia/Jakarta", Enabled: true}
	next, ok := ComputeNext(job, now)
	if !ok {
		t.Fatalf("cron must have next run")
	}
	inLoc := next.In(ResolveLocation("Asia/Jakarta"))
	if inLoc.Hour() != 6 || inLoc.Minute() != 0 {
		t.Fatalf("cron next = %v, want 06:00 WIB", inLoc)
	}
	if _, err := parseCron("bad expr"); err == nil {
		t.Fatalf("bad cron must fail")
	}
	if _, err := parseCron("0 6 * *"); err == nil {
		t.Fatalf("4-field cron must fail")
	}
}

func TestParseFlexibleTime(t *testing.T) {
	loc := ResolveLocation("Asia/Jakarta")
	at, err := ParseFlexibleTime("2026-09-29 18:00", loc)
	if err != nil {
		t.Fatalf("datetime: %v", err)
	}
	if at.In(loc).Hour() != 18 {
		t.Fatalf("hour = %v", at)
	}
	if _, err := ParseFlexibleTime("kapan ya", loc); err == nil {
		t.Fatalf("garbage must fail")
	}
}

func TestDescribeMentionsTimezone(t *testing.T) {
	job := Job{ID: "abc", Name: "stocks", Type: TypeDaily, DailyTime: "06:00", Timezone: "Asia/Jakarta", Enabled: true}
	job.NextRunAt = testNow()
	if s := job.Describe(); !strings.Contains(s, "Asia/Jakarta") {
		t.Fatalf("describe must mention timezone: %q", s)
	}
}
