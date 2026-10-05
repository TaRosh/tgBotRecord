package booking

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Service реализует сценарии записи поверх Repository.
type Service struct {
	repo    Repository
	adminID int64
	now     func() time.Time // подменяется в тестах, чтобы "сейчас" было фиксированным

	onBooked []func(Slot)
}

// NewService создаёт сервис. now — источник текущего времени:
// в main это time.Now, в тестах — функция с фиксированной датой.
func NewService(repo Repository, adminID int64, now func() time.Time) *Service {
	return &Service{repo: repo, adminID: adminID, now: now}
}

// IsAdmin сообщает, является ли пользователь мастером.
func (s *Service) IsAdmin(userID int64) bool { return userID == s.adminID }

// AddSlot создаёт новое окно. Только для мастера.
func (s *Service) AddSlot(ctx context.Context, userID int64, start time.Time) (Slot, error) {
	if !s.IsAdmin(userID) {
		return Slot{}, ErrForbidden
	}
	if !start.After(s.now()) {
		return Slot{}, ErrInPast
	}
	slot, err := s.repo.Create(ctx, start)
	if err != nil {
		return Slot{}, fmt.Errorf("create slot: %w", err)
	}
	return slot, nil
}

// FreeSlots возвращает будущие свободные окна.
func (s *Service) FreeSlots(ctx context.Context) ([]Slot, error) {
	return s.upcoming(ctx, Slot.IsFree)
}

// ClientBookings возвращает будущие записи клиента.
func (s *Service) ClientBookings(ctx context.Context, clientID int64) ([]Slot, error) {
	return s.upcoming(ctx, func(sl Slot) bool {
		return sl.Client != nil && sl.Client.ID == clientID
	})
}

// Schedule возвращает все будущие окна (свободные и занятые). Только для мастера.
func (s *Service) Schedule(ctx context.Context, userID int64) ([]Slot, error) {
	if !s.IsAdmin(userID) {
		return nil, ErrForbidden
	}
	return s.upcoming(ctx, func(Slot) bool { return true })
}

// Book записывает клиента на окно.
func (s *Service) Book(ctx context.Context, slotID int64, client Client) (Slot, error) {
	slot, err := s.repo.Get(ctx, slotID)
	if err != nil {
		return Slot{}, err
	}
	if !slot.Start.After(s.now()) {
		return Slot{}, ErrInPast
	}
	// Проверку "свободно ли" делает сам репозиторий атомарно:
	// если проверить здесь, а записать потом, два клиента успеют
	// занять одно окно между проверкой и записью (race condition).
	now := s.now()
	if err := s.repo.Book(ctx, slotID, client, now); err != nil {
		return Slot{}, err
	}
	slot.Client = &client
	slot.BookedAt = now
	slot.Reminded = false
	for _, hook := range s.onBooked {
		hook(slot)
	}
	return slot, nil
}

// OnBooked регистрирует функцию, которая вызывается после каждой успешной записи
// (например, чтобы запланировать напоминание). Функция не должна блокироваться.
// Регистрировать нужно до начала обработки сообщений.
func (s *Service) OnBooked(hook func(Slot)) {
	s.onBooked = append(s.onBooked, hook)
}

// PendingReminders возвращает записи, которым ещё предстоит напоминание.
func (s *Service) PendingReminders(ctx context.Context, before time.Duration) ([]Slot, error) {
	now := s.now()
	return s.upcoming(ctx, func(sl Slot) bool { return needsReminder(sl, before, now) })
}

// ClaimReminder проверяет, нужно ли ещё напоминание об окне, и атомарно
// отмечает его отправленным. ok == true означает: напоминание "наше", отправляйте.
// Отметка ставится ДО отправки: при сбое отправки клиент не получит напоминание,
// зато никогда не получит его дважды (доставка "не более одного раза").
func (s *Service) ClaimReminder(ctx context.Context, slotID int64, before time.Duration) (slot Slot, ok bool, err error) {
	slot, err = s.repo.Get(ctx, slotID)
	if errors.Is(err, ErrNotFound) {
		return Slot{}, false, nil
	}
	if err != nil {
		return Slot{}, false, err
	}
	if !needsReminder(slot, before, s.now()) {
		return Slot{}, false, nil
	}
	ok, err = s.repo.MarkReminded(ctx, slotID, slot.Client.ID)
	if err != nil || !ok {
		return Slot{}, false, err
	}
	slot.Reminded = true
	return slot, true, nil
}

// needsReminder — единое правило "нужно ли напоминать":
// окно занято, напоминания ещё не было, визит не начался, и клиент записался
// раньше момента напоминания (тому, кто записался за час, напоминать нечего).
func needsReminder(sl Slot, before time.Duration, now time.Time) bool {
	return !sl.IsFree() && !sl.Reminded &&
		sl.Start.After(now) &&
		sl.BookedAt.Before(sl.Start.Add(-before))
}

// Cancel отменяет запись. Клиент может отменить только свою, мастер — любую.
// Возвращает окно в состоянии до отмены, чтобы можно было уведомить второго участника.
func (s *Service) Cancel(ctx context.Context, userID, slotID int64) (Slot, error) {
	slot, err := s.repo.Get(ctx, slotID)
	if err != nil {
		return Slot{}, err
	}
	if slot.IsFree() {
		return Slot{}, ErrSlotNotTaken
	}
	if slot.Client.ID != userID && !s.IsAdmin(userID) {
		// Чужую запись не раскрываем: для клиента её "нет".
		return Slot{}, ErrNotFound
	}
	if err := s.repo.Release(ctx, slotID); err != nil {
		return Slot{}, err
	}
	return slot, nil
}

func (s *Service) upcoming(ctx context.Context, keep func(Slot) bool) ([]Slot, error) {
	all, err := s.repo.ListFrom(ctx, s.now())
	if err != nil {
		return nil, fmt.Errorf("list slots: %w", err)
	}
	// Фильтрация без лишней аллокации: переиспользуем тот же массив.
	res := all[:0]
	for _, sl := range all {
		if keep(sl) {
			res = append(res, sl)
		}
	}
	return res, nil
}
