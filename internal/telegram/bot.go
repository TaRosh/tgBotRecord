package telegram

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

// Run подключается к Telegram и обрабатывает сообщения, пока не отменён ctx.
// Это тонкий адаптер: вся логика в Router, здесь только приём и отправка.
// opts — дополнительные настройки библиотеки (в тестах — адрес фальшивого сервера).
func Run(ctx context.Context, token string, router *Router, log *slog.Logger, opts ...bot.Option) error {
	handler := func(ctx context.Context, b *bot.Bot, update *models.Update) {
		msg := update.Message
		// Работаем только с текстом в личных сообщениях: в группах у бота записи нет.
		if msg == nil || msg.From == nil || msg.Chat.Type != models.ChatTypePrivate {
			return
		}

		log.Debug("message received", "user_id", msg.From.ID, "text", msg.Text)
		resp := router.Handle(ctx, Request{
			UserID: msg.From.ID,
			Name:   displayName(msg.From),
			Text:   msg.Text,
		})

		send(ctx, b, log, msg.Chat.ID, resp.Reply)
		for _, n := range resp.Notify {
			send(ctx, b, log, n.ChatID, n.Text)
		}
	}

	opts = append([]bot.Option{
		bot.WithDefaultHandler(handler),
		bot.WithErrorsHandler(func(err error) { log.Error("telegram", "err", err) }),
	}, opts...)
	b, err := bot.New(token, opts...)
	if err != nil {
		return fmt.Errorf("create telegram bot: %w", err)
	}

	log.Info("telegram bot started")
	b.Start(ctx) // блокируется до отмены ctx
	return nil
}

func send(ctx context.Context, b *bot.Bot, log *slog.Logger, chatID int64, text string) {
	if text == "" {
		return
	}
	if _, err := b.SendMessage(ctx, &bot.SendMessageParams{ChatID: chatID, Text: text}); err != nil {
		// Например, клиент заблокировал бота — это не повод падать.
		log.Warn("send message", "chat_id", chatID, "err", err)
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
