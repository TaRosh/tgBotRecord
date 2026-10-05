// Package storagetest — общий набор тестов ("контракт") для любой реализации
// booking.Repository. Каждая реализация вызывает Run из своего _test.go,
// поэтому память и SQLite гарантированно ведут себя одинаково.
package storagetest

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
)

var base = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

// bookedAt — время, когда клиент "записался" в тестах.
var bookedAt = base.Add(-72 * time.Hour)

// Run запускает все тесты контракта. newRepo должен возвращать пустое хранилище.
func Run(t *testing.T, newRepo func(t *testing.T) booking.Repository) {
	t.Run("CreateAndGet", func(t *testing.T) { testCreateAndGet(t, newRepo(t)) })
	t.Run("CreateDuplicate", func(t *testing.T) { testCreateDuplicate(t, newRepo(t)) })
	t.Run("Get", func(t *testing.T) { testGet(t, newRepo(t)) })
	t.Run("BookAndRelease", func(t *testing.T) { testBookAndRelease(t, newRepo(t)) })
	t.Run("ListFrom", func(t *testing.T) { testListFrom(t, newRepo(t)) })
	t.Run("GetReturnsCopy", func(t *testing.T) { testGetReturnsCopy(t, newRepo(t)) })
	t.Run("BookConcurrent", func(t *testing.T) { testBookConcurrent(t, newRepo(t)) })
	t.Run("BookedAtAndRelease", func(t *testing.T) { testBookedAtAndRelease(t, newRepo(t)) })
	t.Run("MarkReminded", func(t *testing.T) { testMarkReminded(t, newRepo(t)) })
}

func testCreateAndGet(t *testing.T, r booking.Repository) {
	ctx := context.Background()
	s1, err := r.Create(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	s2, err := r.Create(ctx, base.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if s1.ID == 0 || s1.ID == s2.ID {
		t.Fatalf("ids must be unique and non-zero: %d, %d", s1.ID, s2.ID)
	}

	got, err := r.Get(ctx, s1.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != s1.ID || !got.Start.Equal(base) || !got.IsFree() {
		t.Errorf("got %+v, want free slot %d at %v", got, s1.ID, base)
	}
}

func testCreateDuplicate(t *testing.T, r booking.Repository) {
	ctx := context.Background()
	if _, err := r.Create(ctx, base); err != nil {
		t.Fatal(err)
	}
	// То же время, но в другом часовом поясе — это тот же момент.
	sameMoment := base.In(time.FixedZone("UTC+3", 3*3600))
	if _, err := r.Create(ctx, sameMoment); !errors.Is(err, booking.ErrSlotExists) {
		t.Fatalf("err = %v, want ErrSlotExists", err)
	}
}

func testGet(t *testing.T, r booking.Repository) {
	if _, err := r.Get(context.Background(), 42); !errors.Is(err, booking.ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func testBookAndRelease(t *testing.T, r booking.Repository) {
	ctx := context.Background()
	s, err := r.Create(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	anna := booking.Client{ID: 100, Name: "Анна"}

	steps := []struct {
		name    string
		do      func() error
		wantErr error
		wantCl  *booking.Client // ожидаемый клиент после шага
	}{
		{"book free", func() error { return r.Book(ctx, s.ID, anna, bookedAt) }, nil, &anna},
		{"book taken", func() error { return r.Book(ctx, s.ID, booking.Client{ID: 200}, bookedAt) }, booking.ErrSlotTaken, &anna},
		{"release", func() error { return r.Release(ctx, s.ID) }, nil, nil},
		{"book again", func() error { return r.Book(ctx, s.ID, anna, bookedAt) }, nil, &anna},
		{"book unknown", func() error { return r.Book(ctx, 999, anna, bookedAt) }, booking.ErrNotFound, &anna},
		{"release unknown", func() error { return r.Release(ctx, 999) }, booking.ErrNotFound, &anna},
	}
	for _, st := range steps {
		if err := st.do(); !errors.Is(err, st.wantErr) {
			t.Fatalf("%s: err = %v, want %v", st.name, err, st.wantErr)
		}
		got, err := r.Get(ctx, s.ID)
		if err != nil {
			t.Fatal(err)
		}
		switch {
		case st.wantCl == nil && !got.IsFree():
			t.Fatalf("%s: slot must be free, got client %+v", st.name, *got.Client)
		case st.wantCl != nil && (got.Client == nil || *got.Client != *st.wantCl):
			t.Fatalf("%s: client = %+v, want %+v", st.name, got.Client, *st.wantCl)
		}
	}
}

func testListFrom(t *testing.T, r booking.Repository) {
	ctx := context.Background()
	// Добавляем не по порядку — ListFrom обязан отсортировать.
	for _, h := range []int{5, -1, 2, 8, 0} {
		if _, err := r.Create(ctx, base.Add(time.Duration(h)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	tests := []struct {
		name      string
		from      time.Time
		wantHours []int
	}{
		{"from base, inclusive", base, []int{0, 2, 5, 8}},
		{"from the past", base.Add(-24 * time.Hour), []int{-1, 0, 2, 5, 8}},
		{"from the middle", base.Add(3 * time.Hour), []int{5, 8}},
		{"nothing after", base.Add(24 * time.Hour), nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := r.ListFrom(ctx, tt.from)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(tt.wantHours) {
				t.Fatalf("got %d slots, want %d", len(got), len(tt.wantHours))
			}
			for i, h := range tt.wantHours {
				if want := base.Add(time.Duration(h) * time.Hour); !got[i].Start.Equal(want) {
					t.Errorf("slot[%d] = %v, want %v", i, got[i].Start, want)
				}
			}
		})
	}
}

func testGetReturnsCopy(t *testing.T, r booking.Repository) {
	ctx := context.Background()
	s, _ := r.Create(ctx, base)
	if err := r.Book(ctx, s.ID, booking.Client{ID: 1, Name: "Анна"}, bookedAt); err != nil {
		t.Fatal(err)
	}

	got, _ := r.Get(ctx, s.ID)
	got.Client.Name = "Взломщик" // меняем полученное значение

	again, _ := r.Get(ctx, s.ID)
	if again.Client.Name != "Анна" {
		t.Errorf("repo data changed through returned pointer: %q", again.Client.Name)
	}
}

// 50 клиентов одновременно бронируют одно окно — успешно должен ровно один.
// Запускайте с -race.
func testBookConcurrent(t *testing.T, r booking.Repository) {
	ctx := context.Background()
	s, err := r.Create(ctx, base)
	if err != nil {
		t.Fatal(err)
	}

	var (
		wg      sync.WaitGroup
		success atomic.Int32
	)
	for i := range 50 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.Book(ctx, s.ID, booking.Client{ID: int64(i + 1)}, bookedAt)
			switch {
			case err == nil:
				success.Add(1)
			case !errors.Is(err, booking.ErrSlotTaken):
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if n := success.Load(); n != 1 {
		t.Fatalf("successful bookings = %d, want 1", n)
	}
}

// Book сохраняет время записи, Release сбрасывает его вместе с отметкой о напоминании,
// чтобы следующий клиент на это окно тоже получил напоминание.
func testBookedAtAndRelease(t *testing.T, r booking.Repository) {
	ctx := context.Background()
	s, _ := r.Create(ctx, base)
	anna := booking.Client{ID: 100, Name: "Анна"}

	if err := r.Book(ctx, s.ID, anna, bookedAt); err != nil {
		t.Fatal(err)
	}
	got, _ := r.Get(ctx, s.ID)
	if !got.BookedAt.Equal(bookedAt) || got.Reminded {
		t.Fatalf("after Book: BookedAt=%v Reminded=%v", got.BookedAt, got.Reminded)
	}

	if ok, err := r.MarkReminded(ctx, s.ID, anna.ID); err != nil || !ok {
		t.Fatalf("MarkReminded = %v, %v", ok, err)
	}
	if err := r.Release(ctx, s.ID); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Get(ctx, s.ID)
	if !got.IsFree() || !got.BookedAt.IsZero() || got.Reminded {
		t.Fatalf("after Release: %+v", got)
	}

	later := bookedAt.Add(time.Hour)
	if err := r.Book(ctx, s.ID, booking.Client{ID: 200}, later); err != nil {
		t.Fatal(err)
	}
	got, _ = r.Get(ctx, s.ID)
	if !got.BookedAt.Equal(later) || got.Reminded {
		t.Fatalf("after re-Book: BookedAt=%v Reminded=%v", got.BookedAt, got.Reminded)
	}
}

func testMarkReminded(t *testing.T, r booking.Repository) {
	ctx := context.Background()
	booked, _ := r.Create(ctx, base)
	free, _ := r.Create(ctx, base.Add(time.Hour))
	if err := r.Book(ctx, booked.ID, booking.Client{ID: 100}, bookedAt); err != nil {
		t.Fatal(err)
	}

	steps := []struct {
		name     string
		slotID   int64
		clientID int64
		want     bool
	}{
		{"first mark", booked.ID, 100, true},
		{"second mark is a no-op", booked.ID, 100, false},
		{"other client", booked.ID, 200, false},
		{"free slot", free.ID, 100, false},
		{"unknown slot", 999, 100, false},
	}
	for _, st := range steps {
		got, err := r.MarkReminded(ctx, st.slotID, st.clientID)
		if err != nil {
			t.Fatalf("%s: %v", st.name, err)
		}
		if got != st.want {
			t.Errorf("%s: got %v, want %v", st.name, got, st.want)
		}
	}
	if s, _ := r.Get(ctx, booked.ID); !s.Reminded {
		t.Error("slot must be marked as reminded")
	}
}
