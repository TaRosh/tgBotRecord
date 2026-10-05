// Package booking содержит бизнес-логику записи клиентов на свободные окна.
// Пакет ничего не знает ни о Telegram, ни о конкретной базе данных.
package booking

import (
	"context"
	"errors"
	"time"
)

// Client — клиент, записавшийся на окно.
type Client struct {
	ID   int64 // Telegram user ID
	Name string
}

// Slot — окно для записи, которое создаёт мастер.
// Если Client == nil, окно свободно.
type Slot struct {
	ID       int64
	Start    time.Time
	Client   *Client
	BookedAt time.Time // когда клиент записался (нулевое, если окно свободно)
	Reminded bool      // напоминание уже отправлено
}

// IsFree сообщает, свободно ли окно.
func (s Slot) IsFree() bool { return s.Client == nil }

var (
	ErrNotFound     = errors.New("slot not found")
	ErrSlotExists   = errors.New("slot with this time already exists")
	ErrSlotTaken    = errors.New("slot is already taken")
	ErrSlotNotTaken = errors.New("slot is not booked")
	ErrInPast       = errors.New("slot time is in the past")
	ErrForbidden    = errors.New("action not allowed")
)

// Repository — хранилище окон. Интерфейс объявлен здесь, у потребителя,
// а не рядом с реализацией: так booking не зависит от SQLite/памяти,
// и в тестах можно подставить любую реализацию.
type Repository interface {
	// Create сохраняет окно и возвращает его с присвоенным ID.
	// ErrSlotExists — если окно с таким же временем уже есть.
	Create(ctx context.Context, start time.Time) (Slot, error)
	// Get возвращает окно по ID или ErrNotFound.
	Get(ctx context.Context, id int64) (Slot, error)
	// Book атомарно занимает окно, запоминая время записи at:
	// ErrSlotTaken, если оно уже занято.
	Book(ctx context.Context, id int64, client Client, at time.Time) error
	// Release освобождает окно (отмена записи) и сбрасывает отметку о напоминании.
	Release(ctx context.Context, id int64) error
	// MarkReminded атомарно отмечает, что клиенту clientID напомнили об окне id.
	// Возвращает false, если отмечать нечего: окно отменено, занято другим
	// клиентом или напоминание уже отправлено. Это защищает от дублей.
	MarkReminded(ctx context.Context, id, clientID int64) (bool, error)
	// ListFrom возвращает окна с Start >= from, отсортированные по времени.
	ListFrom(ctx context.Context, from time.Time) ([]Slot, error)
}
