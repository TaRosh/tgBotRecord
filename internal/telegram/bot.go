package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/TaRosh/tgBotRecord/internal/booking"
)

// Bot — тонкий адаптер к Telegram: вся логика в Router, здесь только приём и отправка.
type Bot struct {
	api    *bot.Bot
	router *Router
	log    *slog.Logger
}

// NewBot подключается к Telegram (проверяет токен запросом getMe).
// opts — дополнительные настройки библиотеки (в тестах — адрес фальшивого сервера).
func NewBot(token string, router *Router, log *slog.Logger, opts ...bot.Option) (*Bot, error) {
	b := &Bot{router: router, log: log}
	opts = append([]bot.Option{
		bot.WithDefaultHandler(b.handle),
		bot.WithErrorsHandler(func(err error) { log.Error("telegram", "err", err) }),
	}, opts...)

	api, err := bot.New(token, opts...)
	if err != nil {
		return nil, fmt.Errorf("create telegram bot: %w", err)
	}
	b.api = api
	return b, nil
}

// Start обрабатывает сообщения (long polling), пока не отменён ctx.
func (b *Bot) Start(ctx context.Context) {
	b.log.Info("telegram bot started")
	b.api.Start(ctx) // блокируется до отмены ctx
}

// SendReminder отправляет клиенту напоминание о записи.
func (b *Bot) SendReminder(ctx context.Context, slot booking.Slot) error {
	return b.send(ctx, slot.Client.ID, b.router.ReminderText(slot))
}

func (b *Bot) handle(ctx context.Context, _ *bot.Bot, update *models.Update) {
	msg := update.Message
	// Работаем только с текстом в личных сообщениях: в группах у бота записи нет.
	if msg == nil || msg.From == nil || msg.Chat.Type != models.ChatTypePrivate {
		return
	}

	b.log.Debug("message received", "user_id", msg.From.ID, "text", msg.Text)
	resp := b.router.Handle(ctx, Request{
		UserID: msg.From.ID,
		Name:   displayName(msg.From),
		Text:   msg.Text,
	})

	b.sendLogged(ctx, msg.Chat.ID, resp.Reply)
	for _, n := range resp.Notify {
		b.sendLogged(ctx, n.ChatID, n.Text)
	}
}

func (b *Bot) send(ctx context.Context, chatID int64, text string) error {
	_, err := b.api.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text})
	return err
}

// sendLogged отправляет сообщение, а ошибку только логирует:
// например, клиент заблокировал бота — это не повод падать.
func (b *Bot) sendLogged(ctx context.Context, chatID int64, text string) {
	if text == "" {
		return
	}
	if err := b.send(ctx, chatID, text); err != nil {
		b.log.Warn("send message", "chat_id", chatID, "err", err)
	}
}

func displayName(u *models.User) string {
	name := strings.TrimSpace(u.FirstName + " " + u.LastName)
	if u.Username != "" {
		if name == "" {
			return "@" + u.Username
		}
		name += " @" + u.Username
	}
	if name == "" {
		return "без имени"
	}
	return name
}
