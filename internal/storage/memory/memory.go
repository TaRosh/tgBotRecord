// Package memory — потокобезопасное хранилище окон в памяти.
// Данные теряются при перезапуске; используется до подключения SQLite и в тестах.
package memory

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
)

// Repo реализует booking.Repository.
type Repo struct {
	mu     sync.Mutex // бот обрабатывает сообщения в нескольких горутинах
	nextID int64
	slots  map[int64]booking.Slot
}

func New() *Repo {
	return &Repo{nextID: 1, slots: make(map[int64]booking.Slot)}
}

func (r *Repo) Create(_ context.Context, start time.Time) (booking.Slot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for _, s := range r.slots {
		if s.Start.Equal(start) {
			return booking.Slot{}, booking.ErrSlotExists
		}
	}
	s := booking.Slot{ID: r.nextID, Start: start}
	r.slots[s.ID] = s
	r.nextID++
	return s, nil
}

func (r *Repo) Get(_ context.Context, id int64) (booking.Slot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.slots[id]
	if !ok {
		return booking.Slot{}, booking.ErrNotFound
	}
	return copySlot(s), nil
}

func (r *Repo) Book(_ context.Context, id int64, client booking.Client) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Проверка и запись под одной блокировкой — это и есть атомарность.
	s, ok := r.slots[id]
	if !ok {
		return booking.ErrNotFound
	}
	if !s.IsFree() {
		return booking.ErrSlotTaken
	}
	s.Client = &client
	r.slots[id] = s
	return nil
}

func (r *Repo) Release(_ context.Context, id int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.slots[id]
	if !ok {
		return booking.ErrNotFound
	}
	s.Client = nil
	r.slots[id] = s
	return nil
}

func (r *Repo) ListFrom(_ context.Context, from time.Time) ([]booking.Slot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	res := make([]booking.Slot, 0, len(r.slots))
	for _, s := range r.slots {
		if !s.Start.Before(from) {
			res = append(res, copySlot(s))
		}
	}
	// map в Go не хранит порядок, поэтому сортируем: O(n log n).
	slices.SortFunc(res, func(a, b booking.Slot) int { return a.Start.Compare(b.Start) })
	return res, nil
}

// copySlot отдаёт копию, чтобы вызывающий код не мог изменить
// данные хранилища через указатель Client в обход мьютекса.
func copySlot(s booking.Slot) booking.Slot {
	if s.Client != nil {
		c := *s.Client
		s.Client = &c
	}
	return s
}

var _ booking.Repository = (*Repo)(nil) // проверка на этапе компиляции
