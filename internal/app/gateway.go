// Shared Telegram long-polling loop used by both entrypoints.
package app

import (
	"context"
	"errors"
	"log"
	"time"

	"github.com/purujawa06-bot/PURU-AI/internal/telegram"
)

// PollUpdates runs the long-polling loop until ctx ends.
// It exits nil on shutdown and errors after 5 consecutive 409 conflicts.
func PollUpdates(ctx context.Context, tg *telegram.API, handle func(context.Context, *telegram.Update) error) error {
	var offset int64
	conflicts := 0
	for {
		select {
		case <-ctx.Done():
			log.Printf("gateway: shutdown")
			return nil
		default:
		}
		updates, err := tg.GetUpdates(ctx, offset, 40)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			var te *telegram.TelegramError
			if errors.As(err, &te) && te.IsConflict() {
				conflicts++
				if conflicts >= 5 {
					return errors.New("conflict: another instance is using the token")
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
		for i := range updates {
			offset = updates[i].UpdateID + 1
			if err := handle(ctx, &updates[i]); err != nil {
				log.Printf("handle %d: %v", updates[i].UpdateID, err)
			}
		}
	}
}
