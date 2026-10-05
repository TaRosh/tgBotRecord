package telegram

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
)

// Request — входящее сообщение, уже без деталей Telegram API.
type Request struct {
	UserID int64
	Name   string
	Text   string
}

// Notification — сообщение другому пользователю (например, мастеру о новой записи).
type Notification struct {
	ChatID int64
	Text   string
}

// Response — что отправить в ответ. Router не ходит в сеть сам,
// поэтому его можно целиком проверить unit-тестами без Telegram.
type Response struct {
	Reply  string
	Notify []Notification
}

type Router struct {
	svc     *booking.Service
	adminID int64
	loc     *time.Location
	now     func() time.Time
	log     *slog.Logger
}

func NewRouter(svc *booking.Service, adminID int64, loc *time.Location, now func() time.Time, log *slog.Logger) *Router {
	return &Router{svc: svc, adminID: adminID, loc: loc, now: now, log: log}
}

// Handle обрабатывает одно сообщение и возвращает ответ.
func (r *Router) Handle(ctx context.Context, req Request) Response {
	cmd, args, ok := parseCommand(req.Text)
	if !ok {
		return Response{Reply: "Я понимаю только команды. " + r.help(req.UserID)}
	}

	switch cmd {
	case "start":
		return Response{Reply: "Здравствуйте! Я помогу записаться к мастеру.\n\n" + r.help(req.UserID)}
	case "help":
		return Response{Reply: r.help(req.UserID)}
	case "slots":
		return r.slots(ctx)
	case "book":
		return r.book(ctx, req, args)
	case "my":
		return r.my(ctx, req)
	case "cancel":
		return r.cancel(ctx, req, args)
	case "add":
		return r.add(ctx, req, args)
	case "schedule":
		return r.schedule(ctx, req)
	default:
		return Response{Reply: "Неизвестная команда. " + r.help(req.UserID)}
	}
}

// ReminderText — текст напоминания клиенту о предстоящей записи.
func (r *Router) ReminderText(slot booking.Slot) string {
	return fmt.Sprintf("Напоминание: вы записаны на %s.\nЕсли планы изменились, отмените запись: /cancel %d",
		formatTime(slot.Start, r.loc), slot.ID)
}

func (r *Router) help(userID int64) string {
	if r.svc.IsAdmin(userID) {
		return adminHelp
	}
	return clientHelp
}

func (r *Router) slots(ctx context.Context) Response {
	slots, err := r.svc.FreeSlots(ctx)
	if err != nil {
		return r.fail("free slots", err)
	}
	if len(slots) == 0 {
		return Response{Reply: "Свободных окон пока нет."}
	}
	return Response{Reply: formatSlots("Свободные окна:", slots, r.loc, false) + "\n\nЗаписаться: /book N"}
}

func (r *Router) book(ctx context.Context, req Request, args string) Response {
	id, err := parseID(args)
	if err != nil {
		return Response{Reply: "Укажите номер окна, например: /book 3"}
	}
	slot, err := r.svc.Book(ctx, id, booking.Client{ID: req.UserID, Name: req.Name})
	if err != nil {
		return r.fail("book", err)
	}

	resp := Response{Reply: "Вы записаны на " + formatTime(slot.Start, r.loc) + ".\nОтменить: /cancel " + fmt.Sprint(slot.ID)}
	if req.UserID != r.adminID {
		resp.Notify = append(resp.Notify, Notification{
			ChatID: r.adminID,
			Text:   fmt.Sprintf("Новая запись #%d на %s: %s", slot.ID, formatTime(slot.Start, r.loc), formatClient(*slot.Client)),
		})
	}
	return resp
}

func (r *Router) my(ctx context.Context, req Request) Response {
	slots, err := r.svc.ClientBookings(ctx, req.UserID)
	if err != nil {
		return r.fail("client bookings", err)
	}
	if len(slots) == 0 {
		return Response{Reply: "У вас нет записей. Свободные окна: /slots"}
	}
	return Response{Reply: formatSlots("Ваши записи:", slots, r.loc, false)}
}

func (r *Router) cancel(ctx context.Context, req Request, args string) Response {
	id, err := parseID(args)
	if err != nil {
		return Response{Reply: "Укажите номер записи, например: /cancel 3"}
	}
	slot, err := r.svc.Cancel(ctx, req.UserID, id)
	if err != nil {
		return r.fail("cancel", err)
	}

	when := formatTime(slot.Start, r.loc)
	resp := Response{Reply: "Запись #" + fmt.Sprint(slot.ID) + " на " + when + " отменена."}
	// Уведомляем вторую сторону: клиент отменил — мастера, мастер отменил — клиента.
	switch {
	case req.UserID == slot.Client.ID && req.UserID != r.adminID:
		resp.Notify = append(resp.Notify, Notification{
			ChatID: r.adminID,
			Text:   fmt.Sprintf("Клиент %s отменил запись #%d на %s", formatClient(*slot.Client), slot.ID, when),
		})
	case req.UserID != slot.Client.ID:
		resp.Notify = append(resp.Notify, Notification{
			ChatID: slot.Client.ID,
			Text:   "Мастер отменил вашу запись на " + when + ". Выберите другое время: /slots",
		})
	}
	return resp
}

func (r *Router) add(ctx context.Context, req Request, args string) Response {
	if !r.svc.IsAdmin(req.UserID) {
		return r.fail("add", booking.ErrForbidden)
	}
	start, err := parseSlotTime(args, r.now(), r.loc)
	if err != nil {
		return Response{Reply: "Формат: /add ДД.ММ ЧЧ:ММ, например: /add 12.10 14:00"}
	}
	slot, err := r.svc.AddSlot(ctx, req.UserID, start)
	if err != nil {
		return r.fail("add", err)
	}
	return Response{Reply: fmt.Sprintf("Окно #%d добавлено: %s", slot.ID, formatTime(slot.Start, r.loc))}
}

func (r *Router) schedule(ctx context.Context, req Request) Response {
	slots, err := r.svc.Schedule(ctx, req.UserID)
	if err != nil {
		return r.fail("schedule", err)
	}
	if len(slots) == 0 {
		return Response{Reply: "Расписание пустое. Добавьте окно: /add ДД.ММ ЧЧ:ММ"}
	}
	return Response{Reply: formatSlots("Расписание:", slots, r.loc, true)}
}

// fail превращает ошибку в понятный пользователю текст.
// Ожидаемые ошибки бизнес-логики — это нормальный ответ, неожиданные логируем.
func (r *Router) fail(op string, err error) Response {
	var text string
	switch {
	case errors.Is(err, booking.ErrNotFound):
		text = "Окно с таким номером не найдено."
	case errors.Is(err, booking.ErrSlotTaken):
		text = "Это окно уже занято. Свободные окна: /slots"
	case errors.Is(err, booking.ErrSlotNotTaken):
		text = "На это окно никто не записан."
	case errors.Is(err, booking.ErrSlotExists):
		text = "Окно на это время уже есть."
	case errors.Is(err, booking.ErrInPast):
		text = "Это время уже прошло."
	case errors.Is(err, booking.ErrForbidden):
		text = "Эта команда доступна только мастеру."
	default:
		r.log.Error("handle command", "op", op, "err", err)
		text = "Что-то пошло не так, попробуйте позже."
	}
	return Response{Reply: text}
}
