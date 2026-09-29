// PURU-AI lightweight local assistant — Telegram bot.
//
// Config: single JSON (~/.puru/config.json, /root/.puru/config.json for root).
// See example.config.json. Fast boot: model + history + agent only.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
	"github.com/purujawa06-bot/PURU-AI/internal/app"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/health"
	"github.com/purujawa06-bot/PURU-AI/internal/history"
	"github.com/purujawa06-bot/PURU-AI/internal/memory"
	"github.com/purujawa06-bot/PURU-AI/internal/schedule"
	"github.com/purujawa06-bot/PURU-AI/internal/telegram"
)

func main() {
	// Cap heap at 50MB — lightweight profile. Env GOMEMLIMIT wins when set.
	debug.SetMemoryLimit(50 << 20)

	// Reap orphaned exec grandchildren (PID 1 zombie collector, unix only).
	ai.StartReaper()

	cfgPath := flag.String("config", "", "path config.json (default ~/.puru/config.json)")
	flag.Parse()

	cfg, err := config.Load(config.ResolvePath(*cfgPath))
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	hc := &http.Client{} // No timeout, let the API decide

	llm, err := ai.NewModel(cfg, hc)
	if err != nil {
		log.Fatalf("ai model: %v", err)
	}
	histStore := history.New(cfg.HistoryDir())
	memSvc := memory.New(cfg.Workspace)
	memSvc.Model = llm
	tg, err := telegram.New(cfg.TelegramBotToken, hc)
	if err != nil {
		log.Fatalf("telegram: %v", err)
	}
	agentSvc := &ai.Agent{Client: llm, Config: cfg, HTTP: hc, Telegram: tg}
	appSvc := app.New(cfg, tg, histStore, agentSvc, memSvc)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Scheduled tasks runner (Picoclaw cron-like, default Asia/Jakarta).
	go (&schedule.Runner{
		Store: schedule.NewStore(cfg.Workspace),
		Handle: func(runCtx context.Context, job schedule.Job) error {
			return appSvc.RunScheduledJob(runCtx, job)
		},
	}).Start(ctx)
	// Health endpoint only (GET /health) — never blocks Telegram polling.
	go func() {
		addr := health.Addr(cfg.Host, cfg.Port)
		log.Printf("health: %s/health", addr)
		if err := health.Serve(addr); err != nil {
			log.Printf("health: %v", err)
		}
	}()
	me, err := tg.GetMe(ctx)
	if err != nil {
		log.Fatalf("Cannot reach Telegram API: %v", err)
	}
	log.Printf("PURU-AI lightweight on @%s (workspace=%s, max_iter=%d)", me.Username, cfg.Workspace, cfg.MaxIterations)

	if err := tg.DeleteWebhook(ctx, true); err != nil {
		log.Printf("deleteWebhook: %v", err)
	}

	if err := tg.SetCommands(ctx); err != nil {
		log.Printf("setMyCommands: %v", err)
	}

	if err := app.PollUpdates(ctx, tg, appSvc.Handle); err != nil {
		log.Fatalf("gateway: %v", err)
	}
}
