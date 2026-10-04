package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
	"github.com/TaRosh/tgBotRecord/internal/config"
	"github.com/TaRosh/tgBotRecord/internal/storage/memory"
	"github.com/TaRosh/tgBotRecord/internal/telegram"
)

func main() {
	// Контекст отменяется по Ctrl+C или по SIGTERM (его шлёт `docker stop`).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, os.Getenv, os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// run содержит всю логику запуска. main остаётся тонким,
// а run можно вызвать из теста с подменённым окружением и выводом.
func run(ctx context.Context, getenv func(string) string, out io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// JSON-логи удобно читать в `docker logs` и отправлять в системы сбора логов.
	log := slog.New(slog.NewJSONHandler(out, &slog.HandlerOptions{Level: cfg.LogLevel}))

	// Токен не логируем никогда — это секрет.
	log.Info("bot starting",
		"admin_id", cfg.AdminID,
		"timezone", cfg.Location.String(),
		"db_path", cfg.DBPath,
		"log_level", cfg.LogLevel.String(),
	)

	// Сборка зависимостей: хранилище -> бизнес-логика -> Telegram.
	// TODO(шаг 3): заменить memory на SQLite (cfg.DBPath).
	repo := memory.New()
	svc := booking.NewService(repo, cfg.AdminID, time.Now)
	router := telegram.NewRouter(svc, cfg.AdminID, cfg.Location, time.Now, log)

	if err := telegram.Run(ctx, cfg.BotToken, router, log); err != nil {
		return err
	}

	log.Info("bot stopped")
	return nil
}
