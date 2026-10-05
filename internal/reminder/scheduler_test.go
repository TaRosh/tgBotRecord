package reminder

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
	"github.com/TaRosh/tgBotRecord/internal/storage/memory"
)

// В тестах планировщика используется настоящее время, но "за час до записи"
// сжато до миллисекунд: окна ставятся на now + before + несколько десятков мс.
const (
	adminID  = 1
	clientID = 100
	before   = time.Hour
)

type env struct {
	svc   *booking.Service
	sched *Scheduler
	sent  chan booking.Slot
	calls atomic.Int32
}

// newEnv собирает сервис, планировщик и фальшивый Sender.
// sendErr — что возвращать из Sender (для проверки сбоев доставки).
func newEnv(t *testing.T, sendErr error) *env {
	t.Helper()
	e := &env{sent: make(chan booking.Slot, 10)}
	e.svc = booking.NewService(memory.New(), adminID, time.Now)
	send := func(_ context.Context, slot booking.Slot) error {
		e.calls.Add(1)
		e.sent <- slot
		return sendErr
	}
	e.sched = New(e.svc, before, send, slog.New(slog.NewTextHandler(io.Discard, nil)))
	return e
}

// start запускает планировщик и останавливает его в конце теста.
func (e *env) start(t *testing.T) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- e.sched.Run(ctx) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("Run returned %v", err)
			}
		case <-time.After(2 * time.Second):
			t.Error("Run did not stop after context cancel")
		}
	})
}

// book создаёт окно через startIn после "now + before" и записывает на него клиента.
func (e *env) book(t *testing.T, startIn time.Duration) booking.Slot {
	t.Helper()
	ctx := context.Background()
	slot, err := e.svc.AddSlot(ctx, adminID, time.Now().Add(before+startIn))
	if err != nil {
		t.Fatal(err)
	}
	booked, err := e.svc.Book(ctx, slot.ID, booking.Client{ID: clientID, Name: "Анна"})
	if err != nil {
		t.Fatal(err)
	}
	return booked
}

func (e *env) expectReminder(t *testing.T, slotID int64) {
	t.Helper()
	select {
	case got := <-e.sent:
		if got.ID != slotID || got.Client == nil || got.Client.ID != clientID {
			t.Fatalf("reminder for %+v, want slot %d", got, slotID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reminder was not sent")
	}
}

func (e *env) expectNothing(t *testing.T, wait time.Duration) {
	t.Helper()
	select {
	case got := <-e.sent:
		t.Fatalf("unexpected reminder for slot %d", got.ID)
	case <-time.After(wait):
	}
}

func TestReminderSentOnTimeAndOnce(t *testing.T) {
	e := newEnv(t, nil)
	e.svc.OnBooked(e.sched.Schedule)
	e.start(t)

	slot := e.book(t, 300*time.Millisecond) // напомнить через 300 мс

	e.expectNothing(t, 100*time.Millisecond) // не раньше времени
	e.expectReminder(t, slot.ID)
	e.expectNothing(t, 200*time.Millisecond) // и ровно один раз
}

func TestEarlierReminderWakesScheduler(t *testing.T) {
	e := newEnv(t, nil)
	e.svc.OnBooked(e.sched.Schedule)
	e.start(t)

	// Сначала далёкое напоминание: планировщик засыпает надолго.
	e.book(t, time.Hour)
	// Потом близкое: он должен проснуться и пересчитать время сна.
	soon := e.book(t, 100*time.Millisecond)

	e.expectReminder(t, soon.ID)
}

func TestNoReminderWhenBookedInsideWindow(t *testing.T) {
	e := newEnv(t, nil)
	e.svc.OnBooked(e.sched.Schedule)
	e.start(t)

	// Визит через 30 минут при before = 1 час: момент напоминания уже прошёл,
	// клиент только что записался — напоминать незачем.
	e.book(t, -30*time.Minute)

	e.expectNothing(t, 300*time.Millisecond)
}

func TestNoReminderAfterCancel(t *testing.T) {
	e := newEnv(t, nil)
	e.svc.OnBooked(e.sched.Schedule)
	e.start(t)

	slot := e.book(t, 200*time.Millisecond)
	if _, err := e.svc.Cancel(context.Background(), clientID, slot.ID); err != nil {
		t.Fatal(err)
	}

	// Будильник в куче остался, но при срабатывании проверит базу и пропустит окно.
	e.expectNothing(t, 500*time.Millisecond)
}

func TestPendingRemindersLoadedOnStart(t *testing.T) {
	e := newEnv(t, nil)
	// Хук не регистрируем: имитируем запись, сделанную до перезапуска бота.
	slot := e.book(t, 50*time.Millisecond)
	time.Sleep(100 * time.Millisecond) // момент напоминания прошёл, пока бот "лежал"

	e.start(t)
	e.expectReminder(t, slot.ID) // догоняем пропущенное сразу после старта
}

func TestFailedSendIsNotRetried(t *testing.T) {
	e := newEnv(t, errors.New("telegram is down"))
	e.svc.OnBooked(e.sched.Schedule)
	e.start(t)

	slot := e.book(t, 50*time.Millisecond)
	e.expectReminder(t, slot.ID)
	e.expectNothing(t, 300*time.Millisecond)

	// Доставка "не более одного раза": отметка поставлена до отправки.
	if n := e.calls.Load(); n != 1 {
		t.Errorf("send called %d times, want 1", n)
	}
}

func TestRunFailsWhenStorageFails(t *testing.T) {
	svc := booking.NewService(failingRepo{}, adminID, time.Now)
	s := New(svc, before, nil, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Run(context.Background()); err == nil {
		t.Fatal("expected error when pending reminders cannot be loaded")
	}
}

// failingRepo — хранилище, которое не может отдать список окон.
type failingRepo struct{ booking.Repository }

func (failingRepo) ListFrom(context.Context, time.Time) ([]booking.Slot, error) {
	return nil, errors.New("database is down")
}
