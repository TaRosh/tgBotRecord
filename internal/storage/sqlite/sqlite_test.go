package sqlite

import (
	"context"
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
	storagetest.Run(t, func(t *testing.T) booking.Repository { return newRepo(t) })
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
