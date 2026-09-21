// Package telegram is a thin adapter over telego (long-polling Bot API).
// It keeps the bot's small local model (Update/Message/User/Chat) and its
// TelegramError semantics (409 conflict detection, 400 parse-entities check)
// so the app layer stays unchanged.
package telegram

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mymmrac/telego"
	"github.com/mymmrac/telego/telegoapi"
	"github.com/mymmrac/telego/telegoutil"

	"github.com/purujawa06-bot/PURU-AI/internal/ai"
)

// API wraps a telego bot.
type API struct {
	bot *telego.Bot
}

// New builds the bot with the shared HTTP client.
func New(token string, hc *http.Client) (*API, error) {
	return newWithServer(token, hc, "")
}

// newWithServer is New with an overridable API server URL (tests point it at
// an httptest server; production always uses api.telegram.org).
func newWithServer(token string, hc *http.Client, serverURL string) (*API, error) {
	if hc == nil {
		hc = &http.Client{Timeout: 70 * time.Second}
	}
	opts := []telego.BotOption{telego.WithHTTPClient(hc)}
	if serverURL != "" {
		opts = append(opts, telego.WithAPIServer(serverURL))
	}
	bot, err := telego.NewBot(token, opts...)
	if err != nil {
		return nil, err
	}
	return &API{bot: bot}, nil
}

// TelegramError is a non-OK response from the Bot API.
type TelegramError struct {
	Code    int
	Message string
	Method  string
}

func (e *TelegramError) Error() string {
	return "telegram " + e.Method + ": " + strconv.Itoa(e.Code) + " " + e.Message
}

// IsConflict matches the old conflict detection in index.ts.
func (e *TelegramError) IsConflict() bool {
	return e.Code == 409 || strings.Contains(strings.ToLower(e.Message), "conflict")
}

// wrapErr converts a telego API error into TelegramError, preserving Code for
// conflict (409) and parse-entities (400) checks. Non-API errors pass through.
func wrapErr(method string, err error) error {
	if err == nil {
		return nil
	}
	var terr *telegoapi.Error
	if errors.As(err, &terr) {
		return &TelegramError{Code: terr.ErrorCode, Message: terr.Description, Method: method}
	}
	return err
}

// ---------------------------------------------------------------------------
// Data model (subset needed by the bot)
// ---------------------------------------------------------------------------

type Update struct {
	UpdateID int64
	Message  *Message
}

type User struct {
	ID        int64
	Username  string
	FirstName string
	LastName  string
}

type Chat struct {
	ID   int64
	Type string
}

type Message struct {
	MessageID int64
	From      *User
	Chat      *Chat
	Text      string
}

func toUpdate(u telego.Update) Update {
	out := Update{UpdateID: int64(u.UpdateID)}
	if u.Message != nil {
		out.Message = toMessage(u.Message)
	}
	return out
}

func toMessage(m *telego.Message) *Message {
	if m == nil {
		return nil
	}
	out := &Message{
		MessageID: int64(m.MessageID),
		Text:      m.Text,
		Chat:      &Chat{ID: m.Chat.ID, Type: m.Chat.Type},
	}
	if m.From != nil {
		out.From = &User{ID: m.From.ID, Username: m.From.Username, FirstName: m.From.FirstName, LastName: m.From.LastName}
	}
	return out
}

// ---------------------------------------------------------------------------
// Bot API methods used by the app
// ---------------------------------------------------------------------------

func (a *API) GetMe(ctx context.Context) (*User, error) {
	u, err := a.bot.GetMe(ctx)
	if err != nil {
		return nil, wrapErr("getMe", err)
	}
	return &User{ID: u.ID, Username: u.Username}, nil
}

func (a *API) DeleteWebhook(ctx context.Context, dropPending bool) error {
	return wrapErr("deleteWebhook", a.bot.DeleteWebhook(ctx, &telego.DeleteWebhookParams{
		DropPendingUpdates: dropPending,
	}))
}

// botCommands is the registered menu: only /help, /clear, /token.
func botCommands() []telego.BotCommand {
	return []telego.BotCommand{
		{Command: "help", Description: "Bantuan"},
		{Command: "clear", Description: "Hapus history chat"},
		{Command: "token", Description: "Info pemakaian token memory"},
	}
}

// SetCommands registers the bot menu (non-fatal when it fails).
func (a *API) SetCommands(ctx context.Context) error {
	return wrapErr("setMyCommands", a.bot.SetMyCommands(ctx, &telego.SetMyCommandsParams{
		Commands: botCommands(),
	}))
}

// GetTelegramUser fetches a user's name, id and info live via getChat.
// Works for any user id the bot may look up (fails for unknown users).
func (a *API) GetTelegramUser(ctx context.Context, userID int64) (*ai.TelegramUserInfo, error) {
	c, err := a.bot.GetChat(ctx, &telego.GetChatParams{
		ChatID: telego.ChatID{ID: userID},
	})
	if err != nil {
		return nil, wrapErr("getChat", err)
	}
	return &ai.TelegramUserInfo{
		ID: c.ID, Username: c.Username,
		FirstName: c.FirstName, LastName: c.LastName, Bio: c.Bio,
	}, nil
}

// GetUpdates long-polls (timeout seconds). Multiple updates can be returned;
// the caller processes them sequentially.
func (a *API) GetUpdates(ctx context.Context, offset int64, timeout int) ([]Update, error) {
	updates, err := a.bot.GetUpdates(ctx, &telego.GetUpdatesParams{
		Offset:         int(offset),
		Timeout:        timeout,
		AllowedUpdates: []string{telego.MessageUpdates},
	})
	if err != nil {
		return nil, wrapErr("getUpdates", err)
	}
	out := make([]Update, 0, len(updates))
	for _, u := range updates {
		out = append(out, toUpdate(u))
	}
	return out, nil
}

// SendMessage sends text; opts supports "parse_mode" (string) and
// "reply_to_message_id" (int/int64). Returns the sent message id.
func (a *API) SendMessage(ctx context.Context, chatID int64, text string, opts map[string]any) (int64, error) {
	p := &telego.SendMessageParams{
		ChatID: telego.ChatID{ID: chatID},
		Text:   sanitizeText(text),
	}
	if pm, _ := opts["parse_mode"].(string); pm != "" {
		p.ParseMode = pm
	}
	if id := optInt(opts["reply_to_message_id"]); id > 0 {
		p.ReplyParameters = &telego.ReplyParameters{MessageID: int(id)}
	}
	m, err := a.bot.SendMessage(ctx, p)
	if err != nil {
		return 0, wrapErr("sendMessage", err)
	}
	return int64(m.MessageID), nil
}

func (a *API) DeleteMessage(ctx context.Context, chatID int64, messageID int64) error {
	return wrapErr("deleteMessage", a.bot.DeleteMessage(ctx, &telego.DeleteMessageParams{
		ChatID:    telego.ChatID{ID: chatID},
		MessageID: int(messageID),
	}))
}

// EditMessage replaces a sent message's text (live tool-call preview).
func (a *API) EditMessage(ctx context.Context, chatID int64, messageID int64, text string) error {
	_, err := a.bot.EditMessageText(ctx, &telego.EditMessageTextParams{
		ChatID:    telego.ChatID{ID: chatID},
		MessageID: int(messageID),
		Text:      sanitizeText(text),
	})
	return wrapErr("editMessageText", err)
}

// SendFile sends data as a document with a caption.
func (a *API) SendFile(ctx context.Context, chatID int64, filename string, data []byte, caption string) error {
	_, err := a.bot.SendDocument(ctx, &telego.SendDocumentParams{
		ChatID:   telego.ChatID{ID: chatID},
		Document: telegoutil.FileFromBytes(data, filename),
		Caption:  caption,
	})
	return wrapErr("sendDocument", err)
}

func optInt(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	}
	return 0
}

// sanitizeText replaces invalid UTF-8 byte sequences (which can leak in from
// model output or scraped content) with U+FFFD so Telegram never rejects the
// request with "400 text must be encoded in UTF-8".
func sanitizeText(s string) string {
	return strings.ToValidUTF8(s, "\uFFFD")
}
