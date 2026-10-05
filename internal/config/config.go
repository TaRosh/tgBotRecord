// Package config загружает настройки приложения из переменных окружения.
package config

import (
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata" // встраиваем базу часовых поясов: в минимальных Docker-образах её нет
)

const (
	defaultTimezone = "Europe/Moscow"
	defaultDBPath   = "data/bot.db"
	defaultRemind   = 24 * time.Hour
)

// Config — все настройки бота. Заполняется один раз при старте.
type Config struct {
	BotToken string
	AdminID  int64
	Location *time.Location
	DBPath   string
	LogLevel slog.Level
	// RemindBefore — за сколько до записи напомнить клиенту; 0 — не напоминать.
	RemindBefore time.Duration
}

// Load читает конфиг через getenv. В main передаётся os.Getenv,
// в тестах — функция поверх map, чтобы не трогать реальное окружение.
// Возвращает все ошибки валидации сразу, а не только первую.
func Load(getenv func(string) string) (Config, error) {
	var (
		cfg  Config
		errs []error
	)

	cfg.BotToken = strings.TrimSpace(getenv("BOT_TOKEN"))
	if cfg.BotToken == "" {
		errs = append(errs, errors.New("BOT_TOKEN is required"))
	}

	if raw := strings.TrimSpace(getenv("ADMIN_ID")); raw == "" {
		errs = append(errs, errors.New("ADMIN_ID is required"))
	} else if id, err := strconv.ParseInt(raw, 10, 64); err != nil || id <= 0 {
		errs = append(errs, fmt.Errorf("ADMIN_ID must be a positive integer, got %q", raw))
	} else {
		cfg.AdminID = id
	}

	tz := withDefault(getenv("TIMEZONE"), defaultTimezone)
	loc, err := time.LoadLocation(tz)
	if err != nil {
		errs = append(errs, fmt.Errorf("TIMEZONE: unknown time zone %q", tz))
	}
	cfg.Location = loc

	cfg.DBPath = withDefault(getenv("DB_PATH"), defaultDBPath)

	level := withDefault(getenv("LOG_LEVEL"), "info")
	if err := cfg.LogLevel.UnmarshalText([]byte(level)); err != nil {
		errs = append(errs, fmt.Errorf("LOG_LEVEL: unknown level %q (use debug|info|warn|error)", level))
	}

	cfg.RemindBefore = defaultRemind
	if raw := strings.TrimSpace(getenv("REMIND_BEFORE")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < 0 {
			errs = append(errs, fmt.Errorf("REMIND_BEFORE: want duration like 24h or 2h30m (0 disables), got %q", raw))
		}
		cfg.RemindBefore = d
	}

	if len(errs) > 0 {
		return Config{}, errors.Join(errs...)
	}
	return cfg, nil
}

func withDefault(value, def string) string {
	if v := strings.TrimSpace(value); v != "" {
		return v
	}
	return def
}
