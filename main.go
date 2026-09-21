// PURU-AI lightweight local assistant — Telegram bot.
//
// Config: single JSON (~/.puru/config.json, /root/.puru/config.json for root).
// See example.config.json. Fast boot: model + history + agent only.
package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"runtime/debug"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
	"github.com/purujawa06-bot/PURU-AI/internal/app"
	"github.com/purujawa06-bot/PURU-AI/internal/config"
	"github.com/purujawa06-bot/PURU-AI/internal/history"
	"github.com/purujawa06-bot/PURU-AI/internal/memory"
	"github.com/purujawa06-bot/PURU-AI/internal/telegram"
)

func main() {
	// Cap heap at 50MB — lightweight profile. Env GOMEMLIMIT wins when set.
	debug.SetMemoryLimit(50 << 20)

	cfgPath := flag.String("config", "", "path config.json (default ~/.puru/config.json)")
	flag.Parse()

	cfg, err := config.Load(config.ResolvePath(*cfgPath))
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	hc := &http.Client{Timeout: 60 * time.Second}

	llm, err := ai.NewModel(cfg, hc)
	if err != nil {
		log.Fatalf("ai model: %v", err)
	}
	histStore := history.New(cfg.HistoryDir())
	memSvc := memory.New(llm, cfg.MemoryPath())
	agentSvc := &ai.Agent{Client: llm, Config: cfg, HTTP: hc}
	tg := telegram.New(cfg.TelegramBotToken, hc)
	appSvc := app.New(cfg, tg, histStore, agentSvc, memSvc)

	ctx := context.Background()
	me, err := tg.GetMe(ctx)
	if err != nil {
		log.Fatalf("Cannot reach Telegram API: %v", err)
	}
	log.Printf("PURU-AI lightweight on @%s (workspace=%s, max_iter=%d)", me.Username, cfg.Workspace, cfg.MaxIterations)

	if err := tg.DeleteWebhook(ctx, true); err != nil {
		log.Printf("deleteWebhook: %v", err)
	}

	var offset int64
	conflicts := 0
	for {
		updates, err := tg.GetUpdates(ctx, offset, 40)
		if err != nil {
			var te *telegram.TelegramError
			if errors.As(err, &te) && te.IsConflict() {
				conflicts++
				if conflicts >= 5 {
					log.Printf("Conflict %dx — instance lain memakai token. Exit.", conflicts)
					os.Exit(1)
				}
				time.Sleep(10 * time.Second)
				_ = tg.DeleteWebhook(ctx, true)
				offset = 0
				continue
			}
			log.Printf("getUpdates: %v", err)
			time.Sleep(5 * time.Second)
			continue
		}
		conflicts = 0
		for _, u := range updates {
			offset = u.UpdateID + 1
			if err := appSvc.Handle(ctx, &u); err != nil {
				log.Printf("handle %d: %v", u.UpdateID, err)
			}
		}
	}
}
