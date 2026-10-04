package booking

import (
	"context"
	"fmt"
	"time"
)

// Service реализует сценарии записи поверх Repository.
type Service struct {
	repo    Repository
	adminID int64
	now     func() time.Time // подменяется в тестах, чтобы "сейчас" было фиксированным
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
	if err := s.repo.Book(ctx, slotID, client); err != nil {
		return Slot{}, err
	}
	slot.Client = &client
	return slot, nil
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
