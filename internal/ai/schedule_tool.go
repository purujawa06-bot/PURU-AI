// Schedule tool exposes scheduled jobs to the agent.
//
// One tool named "manage_schedule" with an "action" switch keeps the tool count
// small: add, list, get, update, remove, enable, disable. Jobs persist in
// <workspace>/schedule/jobs.json and fire as agent turns via schedule.Runner.
package ai

import (
	"context"
	"strings"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/schedule"
)

func scheduleStoreFor(a *Agent) *schedule.Store {
	ws := ""
	if a != nil && a.Config != nil {
		ws = a.Config.Workspace
	}
	if strings.TrimSpace(ws) == "" {
		return nil
	}
	return schedule.NewStore(ws)
}

func scheduleDefaultTimezone(a *Agent) string {
	if a != nil && a.Config != nil {
		return a.Config.EffectiveTimezone()
	}
	return schedule.DefaultTimezone
}

// buildScheduleTool returns the agent-facing schedule tool.
func buildScheduleTool(a *Agent, opts *ProcessOptions, mk func(string, func(context.Context, map[string]any) (any, error)) *Tool, errVal func(error) (any, error)) *Tool {
	return mk("manage_schedule",
		func(ctx context.Context, args map[string]any) (any, error) {
			store := scheduleStoreFor(a)
			if store == nil {
				return errVal(outputErr("manage_schedule.unavailable"))
			}
			action := strings.ToLower(strings.TrimSpace(argStr(args, "action")))
			now := time.Now().UTC()
			chatID := int64(0)
			userID := int64(0)
			if opts != nil {
				chatID = opts.ChatID
				if opts.User != nil {
					userID = opts.User.ID
				}
			}
			switch action {
			case "list":
				jobs, err := store.List(chatID)
				if err != nil {
					return errVal(err)
				}
				if len(jobs) == 0 {
					return outputText("manage_schedule.no_jobs"), nil
				}
				lines := make([]string, 0, len(jobs))
				for _, j := range jobs {
					lines = append(lines, j.Describe())
				}
				return strings.Join(lines, "\n"), nil
			case "get":
				id := argStr(args, "job_id")
				if id == "" {
					return errVal(outputErr("manage_schedule.job_id_required", "get"))
				}
				job, err := store.Get(id)
				if err != nil {
					return errVal(err)
				}
				return outputf("manage_schedule.get", job.Describe(), job.Prompt), nil
			case "remove":
				id := argStr(args, "job_id")
				if id == "" {
					return errVal(outputErr("manage_schedule.job_id_required", "remove"))
				}
				if err := store.Remove(id); err != nil {
					return errVal(err)
				}
				return outputf("manage_schedule.removed", id), nil
			case "enable", "disable":
				id := argStr(args, "job_id")
				if id == "" {
					return errVal(outputErr("manage_schedule.job_id_required", action))
				}
				job, err := store.SetEnabled(id, action == "enable", now)
				if err != nil {
					return errVal(err)
				}
				return outputf("manage_schedule.toggled", job.ID, action, job.Describe()), nil
			case "add":
				job, err := scheduleJobFromArgs(a, args, chatID, userID, now)
				if err != nil {
					return errVal(err)
				}
				saved, err := store.Add(job, now)
				if err != nil {
					return errVal(err)
				}
				return outputf("manage_schedule.scheduled", saved.Describe()), nil
			case "update":
				id := argStr(args, "job_id")
				if id == "" {
					return errVal(outputErr("manage_schedule.job_id_required", "update"))
				}
				existing, err := store.Get(id)
				if err != nil {
					return errVal(err)
				}
				updated, err := applyScheduleUpdate(a, existing, args, now)
				if err != nil {
					return errVal(err)
				}
				saved, err := store.Update(updated, now)
				if err != nil {
					return errVal(err)
				}
				return outputf("manage_schedule.updated", saved.Describe()), nil
			default:
				return errVal(outputErr("manage_schedule.unsupported_action", action))
			}
		})
}

// scheduleJobFromArgs builds a new job from tool args for add.
func scheduleJobFromArgs(a *Agent, args map[string]any, chatID, userID int64, now time.Time) (schedule.Job, error) {
	prompt := strings.TrimSpace(argStr(args, "prompt"))
	if prompt == "" {
		return schedule.Job{}, outputErr("manage_schedule.prompt_required")
	}
	kind := strings.ToLower(strings.TrimSpace(argStr(args, "type")))
	if kind == "" {
		return schedule.Job{}, outputErr("manage_schedule.type_required")
	}
	tz := strings.TrimSpace(argStr(args, "timezone"))
	if tz == "" {
		tz = scheduleDefaultTimezone(a)
	}
	loc := schedule.ResolveLocation(tz)
	job := schedule.Job{
		Name:     strings.TrimSpace(argStr(args, "name")),
		Prompt:   prompt,
		ChatID:   chatID,
		UserID:   userID,
		Timezone: tz,
		Type:     kind,
	}
	switch kind {
	case schedule.TypeOnce:
		atRaw := strings.TrimSpace(argStr(args, "run_once_at"))
		if atRaw == "" {
			return schedule.Job{}, outputErr("manage_schedule.once_required")
		}
		at, err := schedule.ParseFlexibleTime(atRaw, loc)
		if err != nil {
			return schedule.Job{}, err
		}
		job.At = at.UTC()
	case schedule.TypeEvery:
		secs := argInt(args, "every_seconds")
		if secs == 0 {
			secs = 3600
		}
		job.EverySeconds = secs
	case schedule.TypeDaily:
		daily := strings.TrimSpace(argStr(args, "daily_time"))
		if daily == "" {
			return schedule.Job{}, outputErr("manage_schedule.daily_required")
		}
		if _, _, err := schedule.ParseDailyTime(daily); err != nil {
			return schedule.Job{}, err
		}
		job.DailyTime = daily
		weekdays, err := schedule.ParseWeekdays(args["weekdays"])
		if err != nil {
			return schedule.Job{}, err
		}
		job.Weekdays = weekdays
	case schedule.TypeCron:
		expr := strings.TrimSpace(argStr(args, "cron_expr"))
		if expr == "" {
			return schedule.Job{}, outputErr("manage_schedule.cron_required")
		}
		job.CronExpr = expr
	default:
		return schedule.Job{}, outputErr("manage_schedule.type_invalid")
	}
	endAt, maxRuns, err := scheduleLimitsFromArgs(args, loc, now)
	if err != nil {
		return schedule.Job{}, err
	}
	job.EndAt = endAt
	job.MaxRuns = maxRuns
	return job, nil
}

// applyScheduleUpdate mutates schedule fields of an existing job.
func applyScheduleUpdate(a *Agent, job schedule.Job, args map[string]any, now time.Time) (schedule.Job, error) {
	if v := strings.TrimSpace(argStr(args, "prompt")); v != "" {
		job.Prompt = v
	}
	if v := strings.TrimSpace(argStr(args, "name")); v != "" {
		job.Name = v
	}
	if v := strings.TrimSpace(argStr(args, "timezone")); v != "" {
		job.Timezone = v
	}
	if job.Timezone == "" {
		job.Timezone = scheduleDefaultTimezone(a)
	}
	loc := schedule.ResolveLocation(job.Timezone)
	if v := strings.ToLower(strings.TrimSpace(argStr(args, "type"))); v != "" {
		job.Type = v
		job.At = schedule.Job{}.At
		job.EverySeconds = 0
		job.DailyTime = ""
		job.Weekdays = nil
		job.CronExpr = ""
		switch v {
		case schedule.TypeOnce:
			atRaw := strings.TrimSpace(argStr(args, "run_once_at"))
			if atRaw == "" {
				return schedule.Job{}, outputErr("manage_schedule.once_switch")
			}
			at, err := schedule.ParseFlexibleTime(atRaw, loc)
			if err != nil {
				return schedule.Job{}, err
			}
			job.At = at.UTC()
		case schedule.TypeEvery:
			secs := argInt(args, "every_seconds")
			if secs == 0 {
				secs = 3600
			}
			job.EverySeconds = secs
		case schedule.TypeDaily:
			daily := strings.TrimSpace(argStr(args, "daily_time"))
			if daily == "" {
				return schedule.Job{}, outputErr("manage_schedule.daily_switch")
			}
			job.DailyTime = daily
		case schedule.TypeCron:
			expr := strings.TrimSpace(argStr(args, "cron_expr"))
			if expr == "" {
				return schedule.Job{}, outputErr("manage_schedule.cron_switch")
			}
			job.CronExpr = expr
		default:
			return schedule.Job{}, outputErr("manage_schedule.type_invalid")
		}
	} else {
		// Partial update without type switch: apply provided schedule fields.
		if v := strings.TrimSpace(argStr(args, "run_once_at")); v != "" {
			at, err := schedule.ParseFlexibleTime(v, loc)
			if err != nil {
				return schedule.Job{}, err
			}
			job.Type = schedule.TypeOnce
			job.At = at.UTC()
		}
		if _, ok := args["every_seconds"]; ok && strings.TrimSpace(job.Type) == schedule.TypeEvery {
			if secs := argInt(args, "every_seconds"); secs != 0 {
				job.EverySeconds = secs
			}
		}
		if v := strings.TrimSpace(argStr(args, "daily_time")); v != "" {
			if _, _, err := schedule.ParseDailyTime(v); err != nil {
				return schedule.Job{}, err
			}
			job.Type = schedule.TypeDaily
			job.DailyTime = v
		}
		if _, ok := args["weekdays"]; ok {
			weekdays, err := schedule.ParseWeekdays(args["weekdays"])
			if err != nil {
				return schedule.Job{}, err
			}
			job.Weekdays = weekdays
		}
		if v := strings.TrimSpace(argStr(args, "cron_expr")); v != "" {
			job.Type = schedule.TypeCron
			job.CronExpr = v
		}
	}
	if _, hasEnd := args["end_at"]; hasEnd || args["days"] != nil || args["max_runs"] != nil {
		endAt, maxRuns, err := scheduleLimitsFromArgs(args, loc, now)
		if err != nil {
			return schedule.Job{}, err
		}
		// Only overwrite limits when explicitly provided or cleared.
		if _, ok := args["end_at"]; ok {
			job.EndAt = endAt
		} else if args["days"] != nil && endAt != nil {
			job.EndAt = endAt
		}
		if _, ok := args["max_runs"]; ok {
			job.MaxRuns = maxRuns
		}
	}
	return job, nil
}

// scheduleLimitsFromArgs reads end_at, days, and max_runs into job limits.
func scheduleLimitsFromArgs(args map[string]any, loc *time.Location, now time.Time) (*time.Time, int, error) {
	var endAt *time.Time
	if raw := strings.TrimSpace(argStr(args, "end_at")); raw != "" {
		t, err := schedule.ParseFlexibleTime(raw, loc)
		if err != nil {
			return nil, 0, err
		}
		utc := t.UTC()
		endAt = &utc
	}
	if days := argInt(args, "days"); days != 0 {
		if days < 0 {
			return nil, 0, outputErr("manage_schedule.days_negative")
		}
		if days > 366 {
			return nil, 0, outputErr("manage_schedule.days_max")
		}
		t := now.Add(time.Duration(days) * 24 * time.Hour)
		endAt = &t
	}
	maxRuns := int(argInt(args, "max_runs"))
	if maxRuns < 0 {
		return nil, 0, outputErr("manage_schedule.max_runs_negative")
	}
	if maxRuns > 10000 {
		return nil, 0, outputErr("manage_schedule.max_runs_max")
	}
	return endAt, maxRuns, nil
}
