package sqlite

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
	"github.com/TaRosh/tgBotRecord/internal/storage/storagetest"
)

// newRepo создаёт базу во временной папке теста; Go удалит её сам.
// ":memory:" не подходит: у каждого соединения из пула была бы своя пустая база.
func newRepo(t *testing.T) *Repo {
	t.Helper()
	r, err := Open(context.Background(), filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestRepository(t *testing.T) {
	storagetest.Run(t, func(t *testing.T) booking.Repository {
		t.Helper()
		return newRepo(t)
	})
}

// Ради этого шага всё и делалось: данные переживают перезапуск.
func TestDataSurvivesReopen(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "nested", "dir", "bot.db") // папки создаются сами
	start := time.Date(2026, 10, 12, 14, 0, 0, 0, time.UTC)

	r, err := Open(ctx, path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Create(ctx, start)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Book(ctx, s.ID, booking.Client{ID: 7, Name: "Анна"}); err != nil {
		t.Fatal(err)
	}
	r.Close()

	r, err = Open(ctx, path) // повторный Open не должен ломаться на существующей таблице
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	got, err := r.Get(ctx, s.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Start.Equal(start) || got.Client == nil || got.Client.Name != "Анна" {
		t.Errorf("after reopen got %+v", got)
	}
}

// Ошибки базы не должны маскироваться под бизнес-ошибки (ErrNotFound и т.п.):
// иначе пользователь увидит "окно не найдено", когда на самом деле упала база.
func TestClosedDatabaseErrors(t *testing.T) {
	ctx := context.Background()
	r := newRepo(t)
	r.Close()

	sentinels := []error{booking.ErrNotFound, booking.ErrSlotTaken, booking.ErrSlotExists}
	calls := []struct {
		name string
		call func() error
	}{
		{"Create", func() error { _, err := r.Create(ctx, time.Now()); return err }},
		{"Get", func() error { _, err := r.Get(ctx, 1); return err }},
		{"Book", func() error { return r.Book(ctx, 1, booking.Client{ID: 1}) }},
		{"Release", func() error { return r.Release(ctx, 1) }},
		{"ListFrom", func() error { _, err := r.ListFrom(ctx, time.Now()); return err }},
	}
	for _, c := range calls {
		t.Run(c.name, func(t *testing.T) {
			err := c.call()
			if err == nil {
				t.Fatal("expected error on closed database")
			}
			for _, s := range sentinels {
				if errors.Is(err, s) {
					t.Errorf("db failure reported as business error %v", s)
				}
			}
		})
	}
}

func TestOpenErrors(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "not-a-dir")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
	}{
		{"parent is a file", filepath.Join(file, "bot.db")},
		{"path is a directory", dir},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if r, err := Open(context.Background(), tt.path); err == nil {
				r.Close()
				t.Fatal("expected error")
			}
		})
	}
}
