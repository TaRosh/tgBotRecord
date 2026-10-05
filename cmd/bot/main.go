package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
	"github.com/TaRosh/tgBotRecord/internal/config"
	"github.com/TaRosh/tgBotRecord/internal/reminder"
	"github.com/TaRosh/tgBotRecord/internal/storage/sqlite"
	"github.com/TaRosh/tgBotRecord/internal/telegram"
)

func main() {
	// Контекст отменяется по Ctrl+C или по SIGTERM (его шлёт `docker stop`).
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Getenv, os.Stdout)
	// Не defer: os.Exit завершает процесс сразу и отложенные вызовы не выполняет.
	stop()

	if err != nil {
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
		"remind_before", cfg.RemindBefore.String(),
	)

	// Сборка зависимостей: хранилище -> бизнес-логика -> Telegram.
	repo, err := sqlite.Open(ctx, cfg.DBPath)
	if err != nil {
		return fmt.Errorf("open storage: %w", err)
	}
	defer repo.Close()

	svc := booking.NewService(repo, cfg.AdminID, time.Now)
	router := telegram.NewRouter(svc, cfg.AdminID, cfg.Location, time.Now, log)

	tg, err := telegram.NewBot(cfg.BotToken, router, log)
	if err != nil {
		return err
	}

	// Если планировщик упадёт, останавливаем и бота: работать без
	// напоминаний молча хуже, чем перезапуститься (restart в compose/systemd).
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		wg       sync.WaitGroup
		schedErr error
	)
	if cfg.RemindBefore > 0 {
		sched := reminder.New(svc, cfg.RemindBefore, tg.SendReminder, log)
		svc.OnBooked(sched.Schedule) // до Start: обработчики уже могут вызывать Book
		wg.Go(func() {
			if err := sched.Run(ctx); err != nil {
				schedErr = err
				cancel()
			}
		})
	}

	tg.Start(ctx) // блокируется до отмены ctx
	wg.Wait()     // дожидаемся планировщика, иначе он писал бы в закрытую базу

	if schedErr != nil {
		return fmt.Errorf("reminders: %w", schedErr)
	}
	log.Info("bot stopped")
	return nil
}
