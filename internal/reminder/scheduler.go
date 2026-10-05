package reminder

import (
	"container/heap"
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
)

// Sender доставляет напоминание клиенту (в боте — сообщение в Telegram).
type Sender func(ctx context.Context, slot booking.Slot) error

// Scheduler отправляет напоминание за Before до начала каждой записи.
//
// База данных — источник истины, куча — только расписание будильников.
// Отменённые записи из кучи не удаляются: при срабатывании будильник заново
// проверяет окно в базе и пропускает неактуальное (ленивое удаление).
type Scheduler struct {
	svc    *booking.Service
	before time.Duration
	send   Sender
	log    *slog.Logger

	mu   sync.Mutex // Schedule вызывается из обработчиков сообщений, Run — в своей горутине
	q    queue
	wake chan struct{} // будит Run, когда появился будильник раньше текущего
}

func New(svc *booking.Service, before time.Duration, send Sender, log *slog.Logger) *Scheduler {
	return &Scheduler{
		svc:    svc,
		before: before,
		send:   send,
		log:    log,
		wake:   make(chan struct{}, 1),
	}
}

// Schedule ставит будильник для новой записи. Не блокируется.
func (s *Scheduler) Schedule(slot booking.Slot) {
	s.mu.Lock()
	heap.Push(&s.q, item{At: slot.Start.Add(-s.before), SlotID: slot.ID})
	s.mu.Unlock()

	// Неблокирующая отправка: если сигнал уже ждёт в буфере, второй не нужен.
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

// Run загружает неотправленные напоминания из базы (они могли накопиться,
// пока бот был выключен) и отправляет их по расписанию до отмены ctx.
func (s *Scheduler) Run(ctx context.Context) error {
	pending, err := s.svc.PendingReminders(ctx, s.before)
	if err != nil {
		return fmt.Errorf("load pending reminders: %w", err)
	}
	for _, slot := range pending {
		s.Schedule(slot)
	}
	s.log.Info("reminder scheduler started", "pending", len(pending), "before", s.before.String())

	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		// Спим до ближайшего будильника. Если очередь пуста — до сигнала wake.
		timer.Stop()
		s.mu.Lock()
		next, ok := s.q.peek()
		s.mu.Unlock()
		var alarm <-chan time.Time
		if ok {
			timer.Reset(time.Until(next.At))
			alarm = timer.C
		}

		select {
		case <-ctx.Done():
			s.log.Info("reminder scheduler stopped")
			return nil
		case <-s.wake:
			// Появился новый будильник — пересчитаем, до какого момента спать.
		case <-alarm:
			s.mu.Lock()
			due := s.q.popDue(time.Now())
			s.mu.Unlock()
			for _, it := range due {
				s.fire(ctx, it.SlotID)
			}
		}
	}
}

func (s *Scheduler) fire(ctx context.Context, slotID int64) {
	slot, ok, err := s.svc.ClaimReminder(ctx, slotID, s.before)
	if err != nil {
		s.log.Error("claim reminder", "slot_id", slotID, "err", err)
		return
	}
	if !ok {
		s.log.Debug("reminder skipped", "slot_id", slotID) // отменено или уже отправлено
		return
	}
	if err := s.send(ctx, slot); err != nil {
		s.log.Warn("send reminder", "slot_id", slotID, "client_id", slot.Client.ID, "err", err)
		return
	}
	s.log.Info("reminder sent", "slot_id", slotID, "client_id", slot.Client.ID)
}
