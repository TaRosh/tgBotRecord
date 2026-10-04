package memory

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

func TestListFromSortedAndFiltered(t *testing.T) {
	ctx := context.Background()
	r := New()
	// Добавляем не по порядку — map порядок не хранит, ListFrom обязан отсортировать.
	for _, h := range []int{5, -1, 2, 8, 0} {
		if _, err := r.Create(ctx, base.Add(time.Duration(h)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}

	got, err := r.ListFrom(ctx, base)
	if err != nil {
		t.Fatal(err)
	}
	wantHours := []int{0, 2, 5, 8} // -1 отфильтровано, base включительно
	if len(got) != len(wantHours) {
		t.Fatalf("got %d slots, want %d", len(got), len(wantHours))
	}
	for i, h := range wantHours {
		if want := base.Add(time.Duration(h) * time.Hour); !got[i].Start.Equal(want) {
			t.Errorf("slot[%d] = %v, want %v", i, got[i].Start, want)
		}
	}
}

func TestGetReturnsCopy(t *testing.T) {
	ctx := context.Background()
	r := New()
	s, _ := r.Create(ctx, base)
	_ = r.Book(ctx, s.ID, booking.Client{ID: 1, Name: "Анна"})

	got, _ := r.Get(ctx, s.ID)
	got.Client.Name = "Взломщик" // меняем копию

	again, _ := r.Get(ctx, s.ID)
	if again.Client.Name != "Анна" {
		t.Errorf("repo data changed through returned pointer: %q", again.Client.Name)
	}
}

// Главный тест на гонку: 100 клиентов одновременно бронируют одно окно,
// успешно должен ровно один. Запускайте с -race.
func TestBookConcurrent(t *testing.T) {
	ctx := context.Background()
	r := New()
	s, _ := r.Create(ctx, base)

	var (
		wg      sync.WaitGroup
		success atomic.Int32
	)
	for i := range 100 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := r.Book(ctx, s.ID, booking.Client{ID: int64(i + 1)})
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
