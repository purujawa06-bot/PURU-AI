// Package schedule provides lightweight scheduled tasks, Picoclaw cron-like.
//
// Jobs persist in <workspace>/schedule/jobs.json and fire as agent turns.
// Supported types: once (wall-clock), every (interval seconds), daily
// (HH:MM with optional weekdays), cron (5-field expression). All wall-clock
// times resolve in the job timezone, default Asia/Jakarta.
package schedule

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// DefaultTimezone is used when a job or config leaves timezone empty.
const DefaultTimezone = "Asia/Jakarta"

// Job types.
const (
	TypeOnce  = "once"
	TypeEvery = "every"
	TypeDaily = "daily"
	TypeCron  = "cron"
)

// Job is one scheduled task. Prompt runs as an agent turn when due.
type Job struct {
	ID          string     `json:"id"`
	Name        string     `json:"name"`
	Prompt      string     `json:"prompt"`
	ChatID      int64      `json:"chat_id"`
	UserID      int64      `json:"user_id"`
	Timezone    string     `json:"timezone"`
	Type        string     `json:"type"`
	At          time.Time  `json:"at,omitempty"`
	EverySeconds int64     `json:"every_seconds,omitempty"`
	DailyTime   string     `json:"daily_time,omitempty"`
	Weekdays    []string   `json:"weekdays,omitempty"`
	CronExpr    string     `json:"cron_expr,omitempty"`
	EndAt       *time.Time `json:"end_at,omitempty"`
	MaxRuns     int        `json:"max_runs,omitempty"`
	Enabled     bool       `json:"enabled"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	LastRunAt   *time.Time `json:"last_run_at,omitempty"`
	NextRunAt   time.Time  `json:"next_run_at"`
	RunCount    int        `json:"run_count"`
}

// Store persists jobs in <workspace>/schedule/jobs.json.
type Store struct {
	mu   sync.Mutex
	path string
}

// NewStore returns a store rooted at workspace (no I/O until first call).
func NewStore(workspace string) *Store {
	return &Store{path: filepath.Join(workspace, "schedule", "jobs.json")}
}

// JobsPath reports the backing file, useful for logs.
func (s *Store) JobsPath() string {
	if s == nil {
		return ""
	}
	return s.path
}

func (s *Store) loadLocked() ([]Job, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return nil, nil
	}
	var jobs []Job
	if err := json.Unmarshal(raw, &jobs); err != nil {
		return nil, fmt.Errorf("parse jobs file: %w", err)
	}
	return jobs, nil
}

func (s *Store) saveLocked(jobs []Job) error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(jobs, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// List returns jobs scoped to chatID (0 returns all), ordered by next run.
func (s *Store) List(chatID int64) ([]Job, error) {
	if s == nil {
		return nil, fmt.Errorf("schedule store not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	out := make([]Job, 0, len(jobs))
	for _, j := range jobs {
		if chatID != 0 && j.ChatID != 0 && j.ChatID != chatID {
			continue
		}
		out = append(out, j)
	}
	sort.Slice(out, func(i, k int) bool {
		if out[i].NextRunAt.IsZero() {
			return false
		}
		if out[k].NextRunAt.IsZero() {
			return true
		}
		return out[i].NextRunAt.Before(out[k].NextRunAt)
	})
	return out, nil
}

// Get fetches one job by id.
func (s *Store) Get(id string) (Job, error) {
	if s == nil {
		return Job{}, fmt.Errorf("schedule store not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadLocked()
	if err != nil {
		return Job{}, err
	}
	for _, j := range jobs {
		if j.ID == strings.TrimSpace(id) {
			return j, nil
		}
	}
	return Job{}, fmt.Errorf("job %q not found", strings.TrimSpace(id))
}

// Add validates, stamps, and persists a new job.
func (s *Store) Add(job Job, now time.Time) (Job, error) {
	if s == nil {
		return Job{}, fmt.Errorf("schedule store not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if err := Validate(job); err != nil {
		return Job{}, err
	}
	job.ID = strings.TrimSpace(job.ID)
	if job.ID == "" {
		job.ID = newID()
	}
	job.Timezone = NormalizeTimezone(job.Timezone)
	job.Weekdays = NormalizeWeekdays(job.Weekdays)
	job.Enabled = true
	job.CreatedAt = now
	job.UpdatedAt = now
	job.RunCount = 0
	job.LastRunAt = nil
	if strings.TrimSpace(job.Type) == TypeOnce && !job.At.After(now) {
		return Job{}, fmt.Errorf("run_once_at must be in the future")
	}
	next, ok := ComputeNext(job, now)
	if !ok {
		return Job{}, fmt.Errorf("schedule has no future run (check time, weekdays, or end date)")
	}
	job.NextRunAt = next

	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadLocked()
	if err != nil {
		return Job{}, err
	}
	for _, j := range jobs {
		if j.ID == job.ID {
			return Job{}, fmt.Errorf("job id %q already exists", job.ID)
		}
	}
	jobs = append(jobs, job)
	if err := s.saveLocked(jobs); err != nil {
		return Job{}, err
	}
	return job, nil
}

// Update replaces a job by id, preserving run state unless rescheduled.
// Callers must load with Get, mutate schedule fields, then call Update.
func (s *Store) Update(job Job, now time.Time) (Job, error) {
	if s == nil {
		return Job{}, fmt.Errorf("schedule store not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	job.ID = strings.TrimSpace(job.ID)
	if job.ID == "" {
		return Job{}, fmt.Errorf("job id is required")
	}
	if err := Validate(job); err != nil {
		return Job{}, err
	}
	job.Timezone = NormalizeTimezone(job.Timezone)
	job.Weekdays = NormalizeWeekdays(job.Weekdays)
	job.UpdatedAt = now
	next, ok := ComputeNext(job, now)
	if job.Enabled && !ok {
		return Job{}, fmt.Errorf("updated schedule has no future run")
	}
	if ok {
		job.NextRunAt = next
	} else {
		job.NextRunAt = time.Time{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadLocked()
	if err != nil {
		return Job{}, err
	}
	found := false
	for i, j := range jobs {
		if j.ID == job.ID {
			jobs[i] = job
			found = true
			break
		}
	}
	if !found {
		return Job{}, fmt.Errorf("job %q not found", job.ID)
	}
	if err := s.saveLocked(jobs); err != nil {
		return Job{}, err
	}
	return job, nil
}

// Remove deletes one job by id.
func (s *Store) Remove(id string) error {
	if s == nil {
		return fmt.Errorf("schedule store not configured")
	}
	id = strings.TrimSpace(id)
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadLocked()
	if err != nil {
		return err
	}
	kept := jobs[:0]
	found := false
	for _, j := range jobs {
		if j.ID == id {
			found = true
			continue
		}
		kept = append(kept, j)
	}
	if !found {
		return fmt.Errorf("job %q not found", id)
	}
	return s.saveLocked(kept)
}

// SetEnabled toggles a job and recomputes its next run when enabling.
func (s *Store) SetEnabled(id string, enabled bool, now time.Time) (Job, error) {
	job, err := s.Get(id)
	if err != nil {
		return Job{}, err
	}
	job.Enabled = enabled
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return s.Update(job, now)
}

// Due returns enabled jobs whose next run is at or before now.
func (s *Store) Due(now time.Time) ([]Job, error) {
	if s == nil {
		return nil, fmt.Errorf("schedule store not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadLocked()
	if err != nil {
		return nil, err
	}
	var out []Job
	for _, j := range jobs {
		if !j.Enabled || j.NextRunAt.IsZero() || j.NextRunAt.After(now) {
			continue
		}
		if j.MaxRuns > 0 && j.RunCount >= j.MaxRuns {
			continue
		}
		if j.EndAt != nil && !now.Before(*j.EndAt) {
			continue
		}
		out = append(out, j)
	}
	return out, nil
}

// MarkFired records one firing and returns the updated job.
// Deleted is true when a one-shot or exhausted job was removed.
func (s *Store) MarkFired(id string, now time.Time) (updated Job, deleted bool, err error) {
	if s == nil {
		return Job{}, false, fmt.Errorf("schedule store not configured")
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs, err := s.loadLocked()
	if err != nil {
		return Job{}, false, err
	}
	idx := -1
	for i, j := range jobs {
		if j.ID == strings.TrimSpace(id) {
			idx = i
			break
		}
	}
	if idx < 0 {
		return Job{}, false, fmt.Errorf("job %q not found", strings.TrimSpace(id))
	}
	job := jobs[idx]
	job.RunCount++
	job.LastRunAt = &now
	job.UpdatedAt = now
	if job.Type == TypeOnce || (job.MaxRuns > 0 && job.RunCount >= job.MaxRuns) {
		jobs = append(jobs[:idx], jobs[idx+1:]...)
		if err := s.saveLocked(jobs); err != nil {
			return Job{}, false, err
		}
		job.NextRunAt = time.Time{}
		return job, true, nil
	}
	next, ok := ComputeNext(job, now)
	if !ok {
		jobs = append(jobs[:idx], jobs[idx+1:]...)
		if err := s.saveLocked(jobs); err != nil {
			return Job{}, false, err
		}
		job.NextRunAt = time.Time{}
		return job, true, nil
	}
	job.NextRunAt = next
	jobs[idx] = job
	if err := s.saveLocked(jobs); err != nil {
		return Job{}, false, err
	}
	return job, false, nil
}

// Validate checks job fields without touching disk.
func Validate(job Job) error {
	if strings.TrimSpace(job.Prompt) == "" {
		return fmt.Errorf("prompt is required (what should run)")
	}
	if _, err := loadTimezone(job.Timezone); err != nil {
		return err
	}
	switch strings.TrimSpace(job.Type) {
	case TypeOnce:
		if job.At.IsZero() {
			return fmt.Errorf("run_once_at is required for one-time jobs")
		}
	case TypeEvery:
		if job.EverySeconds < 60 {
			return fmt.Errorf("every_seconds must be at least 60")
		}
		if job.EverySeconds > 366*24*3600 {
			return fmt.Errorf("every_seconds must be under one year")
		}
	case TypeDaily:
		if _, _, err := ParseDailyTime(job.DailyTime); err != nil {
			return err
		}
		if _, err := normalizeWeekdayList(job.Weekdays); err != nil {
			return err
		}
	case TypeCron:
		if strings.TrimSpace(job.CronExpr) == "" {
			return fmt.Errorf("cron_expr is required for cron jobs")
		}
		if _, err := parseCron(strings.TrimSpace(job.CronExpr)); err != nil {
			return err
		}
	default:
		return fmt.Errorf("type must be once, every, daily, or cron")
	}
	if job.MaxRuns < 0 {
		return fmt.Errorf("max_runs cannot be negative")
	}
	if job.MaxRuns > 10000 {
		return fmt.Errorf("max_runs must be under 10000")
	}
	return nil
}

// ComputeNext returns the next firing time after now.
// False means the job is exhausted (spent, past its end, or invalid).
func ComputeNext(job Job, now time.Time) (time.Time, bool) {
	if job.MaxRuns > 0 && job.RunCount >= job.MaxRuns {
		return time.Time{}, false
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if job.EndAt != nil && !now.Before(*job.EndAt) {
		return time.Time{}, false
	}
	loc := ResolveLocation(job.Timezone)
	switch strings.TrimSpace(job.Type) {
	case TypeOnce:
		if job.RunCount > 0 || job.At.IsZero() {
			return time.Time{}, false
		}
		if job.EndAt != nil && !job.At.Before(*job.EndAt) {
			return time.Time{}, false
		}
		return job.At, true
	case TypeEvery:
		if job.EverySeconds < 60 {
			return time.Time{}, false
		}
		base := job.CreatedAt
		if job.LastRunAt != nil && !job.LastRunAt.IsZero() {
			base = *job.LastRunAt
		}
		if base.IsZero() {
			return now.Add(time.Duration(job.EverySeconds) * time.Second), true
		}
		next := base.Add(time.Duration(job.EverySeconds) * time.Second)
		if !next.After(now) {
			return now, true
		}
		if job.EndAt != nil && !next.Before(*job.EndAt) {
			return time.Time{}, false
		}
		return next, true
	case TypeDaily:
		hour, min, err := ParseDailyTime(job.DailyTime)
		if err != nil {
			return time.Time{}, false
		}
		weekdays, err := normalizeWeekdayList(job.Weekdays)
		if err != nil {
			return time.Time{}, false
		}
		next := NextDailyAfter(hour, min, weekdays, loc, now)
		if next.IsZero() {
			return time.Time{}, false
		}
		if job.EndAt != nil && !next.Before(*job.EndAt) {
			return time.Time{}, false
		}
		return next, true
	case TypeCron:
		expr := strings.TrimSpace(job.CronExpr)
		if expr == "" {
			return time.Time{}, false
		}
		next, err := NextCronAfter(expr, loc, now)
		if err != nil {
			return time.Time{}, false
		}
		if job.EndAt != nil && !next.Before(*job.EndAt) {
			return time.Time{}, false
		}
		return next, true
	default:
		return time.Time{}, false
	}
}

// Describe renders one job in a compact English line for chat and tool output.
func (j Job) Describe() string {
	status := "enabled"
	if !j.Enabled {
		status = "disabled"
	}
	when := j.NextRunAt.UTC().Format("2006-01-02 15:04 UTC")
	if !j.NextRunAt.IsZero() {
		loc := ResolveLocation(j.Timezone)
		when = j.NextRunAt.In(loc).Format("2006-01-02 15:04") + " " + j.Timezone
	}
	kind := strings.TrimSpace(j.Type)
	detail := kind
	switch kind {
	case TypeOnce:
		detail = "once at " + when
	case TypeEvery:
		detail = fmt.Sprintf("every %ds", j.EverySeconds)
	case TypeDaily:
		detail = "daily " + strings.TrimSpace(j.DailyTime)
		if len(j.Weekdays) > 0 {
			detail += " on " + strings.Join(NormalizeWeekdays(j.Weekdays), ",")
		}
	case TypeCron:
		detail = "cron " + strings.TrimSpace(j.CronExpr)
	}
	extra := ""
	if j.MaxRuns > 0 {
		extra = fmt.Sprintf(", %d/%d runs", j.RunCount, j.MaxRuns)
	} else if j.RunCount > 0 {
		extra = fmt.Sprintf(", %d runs", j.RunCount)
	}
	name := strings.TrimSpace(j.Name)
	if name == "" {
		name = "scheduled task"
	}
	return fmt.Sprintf("%s (%s): %s, next %s, %s%s", j.ID, name, detail, when, status, extra)
}

func newID() string {
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// NormalizeTimezone returns the effective IANA name, defaulting empty input.
func NormalizeTimezone(tz string) string {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return DefaultTimezone
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return DefaultTimezone
	}
	return tz
}

// ValidateTimezone reports whether tz is empty (uses default) or loadable.
func ValidateTimezone(tz string) error {
	_, err := loadTimezone(tz)
	return err
}

func loadTimezone(tz string) (*time.Location, error) {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		tz = DefaultTimezone
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return nil, fmt.Errorf("unknown timezone %q (use IANA like Asia/Jakarta)", tz)
	}
	return loc, nil
}

// ResolveLocation never fails: unknown names fall back to the default,
// then UTC as a last resort.
func ResolveLocation(tz string) *time.Location {
	if loc, err := loadTimezone(tz); err == nil {
		return loc
	}
	if loc, err := time.LoadLocation(DefaultTimezone); err == nil {
		return loc
	}
	return time.UTC
}

// ParseDailyTime accepts H, HH, HH:MM in 24-hour form.
func ParseDailyTime(s string) (int, int, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, 0, fmt.Errorf("daily_time is required (HH:MM 24-hour)")
	}
	var hour, min int
	parts := strings.Split(s, ":")
	if len(parts) == 1 {
		var h int
		if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
			return 0, 0, fmt.Errorf("daily_time %q must look like 06:00", s)
		}
		hour, min = h, 0
	} else if len(parts) == 2 {
		if _, err := fmt.Sscanf(parts[0], "%d", &hour); err != nil {
			return 0, 0, fmt.Errorf("daily_time %q must look like 06:00", s)
		}
		if _, err := fmt.Sscanf(parts[1], "%d", &min); err != nil {
			return 0, 0, fmt.Errorf("daily_time %q must look like 06:00", s)
		}
	} else {
		return 0, 0, fmt.Errorf("daily_time %q must look like 06:00", s)
	}
	if hour < 0 || hour > 23 || min < 0 || min > 59 {
		return 0, 0, fmt.Errorf("daily_time %q is out of range", s)
	}
	return hour, min, nil
}

// ParseFlexibleTime reads wall-clock times in loc. Accepted layouts:
// RFC3339, "2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02",
// and "15:04" (today, or tomorrow when already passed).
func ParseFlexibleTime(s string, loc *time.Location) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, fmt.Errorf("time is required")
	}
	if loc == nil {
		loc = ResolveLocation("")
	}
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	for _, layout := range []string{"2006-01-02 15:04:05", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	if hour, min, err := ParseDailyTime(s); err == nil {
		now := time.Now().In(loc)
		candidate := time.Date(now.Year(), now.Month(), now.Day(), hour, min, 0, 0, loc)
		if !candidate.After(now) {
			candidate = candidate.Add(24 * time.Hour)
		}
		return candidate, nil
	}
	return time.Time{}, fmt.Errorf("time %q must be RFC3339 or YYYY-MM-DD HH:MM in %s", s, loc.String())
}

// ParseWeekdays accepts a comma string, []string, or []any of weekday names.
func ParseWeekdays(v any) ([]string, error) {
	switch t := v.(type) {
	case nil:
		return nil, nil
	case string:
		if strings.TrimSpace(t) == "" {
			return nil, nil
		}
		return normalizeWeekdayList(strings.Split(t, ","))

	case []string:
		return normalizeWeekdayList(t)
	case []any:
		days := make([]string, 0, len(t))
		for _, item := range t {
			name, _ := item.(string)
			days = append(days, name)
		}
		return normalizeWeekdayList(days)
	default:
		return nil, fmt.Errorf("weekdays must be an array like [\"mon\",\"wed\",\"fri\"]")
	}
}

// NormalizeWeekdays lowercases and dedupes weekday names.
func NormalizeWeekdays(days []string) []string {
	out, _ := normalizeWeekdayList(days)
	return out
}

var weekdayIndex = map[string]time.Weekday{
	"sun": time.Sunday, "sunday": time.Sunday,
	"mon": time.Monday, "monday": time.Monday,
	"tue": time.Tuesday, "tues": time.Tuesday, "tuesday": time.Tuesday,
	"wed": time.Wednesday, "wednesday": time.Wednesday,
	"thu": time.Thursday, "thur": time.Thursday, "thurs": time.Thursday, "thursday": time.Thursday,
	"fri": time.Friday, "friday": time.Friday,
	"sat": time.Saturday, "saturday": time.Saturday,
}

func normalizeWeekdayList(days []string) ([]string, error) {
	seen := map[string]bool{}
	var out []string
	for _, d := range days {
		key := strings.ToLower(strings.TrimSpace(d))
		if key == "" {
			continue
		}
		if _, ok := weekdayIndex[key]; !ok {
			return nil, fmt.Errorf("unknown weekday %q (use mon,tue,wed,thu,fri,sat,sun)", d)
		}
		short := key[:3]
		if short == "tue" && (key == "tues" || key == "tuesday") {
			short = "tue"
		}
		if !seen[short] {
			seen[short] = true
			out = append(out, short)
		}
	}
	return out, nil
}

// NextDailyAfter returns the next HH:MM occurrence strictly after now.
func NextDailyAfter(hour, min int, weekdays []string, loc *time.Location, after time.Time) time.Time {
	if loc == nil {
		loc = time.UTC
	}
	inLoc := after.In(loc)
	allowed := map[time.Weekday]bool{}
	if len(weekdays) == 0 {
		for d := time.Sunday; d <= time.Saturday; d++ {
			allowed[d] = true
		}
	} else {
		for _, name := range weekdays {
			if wd, ok := weekdayIndex[strings.ToLower(name)]; ok {
				allowed[wd] = true
			}
		}
	}
	for day := 0; day < 366; day++ {
		base := inLoc.Add(time.Duration(day) * 24 * time.Hour)
		candidate := time.Date(base.Year(), base.Month(), base.Day(), hour, min, 0, 0, loc)
		if !candidate.After(after) {
			continue
		}
		if allowed[candidate.Weekday()] {
			return candidate
		}
	}
	return time.Time{}
}
