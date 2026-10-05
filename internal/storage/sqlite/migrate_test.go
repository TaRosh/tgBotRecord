package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
)

// База, созданная версией бота до появления миграций (схема v1, user_version = 0),
// должна обновиться без потери данных: окна и записи клиентов сохраняются.
func TestMigrateLegacyDatabase(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")

	legacy, err := sql.Open("sqlite", "file:"+path)
	if err != nil {
		t.Fatal(err)
	}
	start := time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)
	for _, q := range []string{
		`CREATE TABLE slots (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			start_at INTEGER NOT NULL UNIQUE,
			client_id INTEGER,
			client_name TEXT)`,
		`INSERT INTO slots (start_at, client_id, client_name) VALUES (` +
			itoa(start.Unix()) + `, 7, 'Анна')`,
	} {
		if _, err := legacy.ExecContext(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	legacy.Close()

	r, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("migrate legacy db: %v", err)
	}
	defer r.Close()

	got, err := r.Get(ctx, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Start.Equal(start) || got.Client == nil || got.Client.Name != "Анна" {
		t.Errorf("data lost after migration: %+v", got)
	}
	// У старых записей нет времени записи (нулевое время) и нет отметки о напоминании.
	// Нулевое время раньше любого момента, поэтому им напоминание придёт.
	if !got.BookedAt.IsZero() || got.Reminded {
		t.Errorf("unexpected defaults after migration: %+v", got)
	}
	// Новые колонки работают.
	if ok, err := r.MarkReminded(ctx, 1, 7); err != nil || !ok {
		t.Errorf("MarkReminded after migration = %v, %v", ok, err)
	}

	assertVersion(t, r.db, len(migrations))
}

// Повторный Open не применяет миграции заново (иначе ALTER TABLE упал бы
// с "duplicate column").
func TestMigrateIdempotent(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "bot.db")
	for range 3 {
		r, err := Open(ctx, path)
		if err != nil {
			t.Fatal(err)
		}
		assertVersion(t, r.db, len(migrations))
		r.Close()
	}
	var _ booking.Repository = (*Repo)(nil)
}

func assertVersion(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	var v int
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		t.Fatal(err)
	}
	if v != want {
		t.Errorf("user_version = %d, want %d", v, want)
	}
}

func itoa(n int64) string { return fmt.Sprint(n) }
