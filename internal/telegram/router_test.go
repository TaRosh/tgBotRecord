package telegram

import (
	"context"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
	"github.com/TaRosh/tgBotRecord/internal/storage/memory"
)

const (
	adminID  = 1
	clientID = 100
	otherID  = 200
)

// newRouter собирает роутер с настоящими сервисом и in-memory хранилищем.
// "Сейчас" — 5 октября 2026, 12:00 UTC.
func newRouter(t *testing.T) *Router {
	t.Helper()
	now := func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }
	svc := booking.NewService(memory.New(), adminID, now)
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	return NewRouter(svc, adminID, time.UTC, now, log)
}

// say — короткий помощник для отправки сообщения от пользователя.
func say(r *Router, userID int64, text string) Response {
	return r.Handle(context.Background(), Request{UserID: userID, Name: "Анна", Text: text})
}

func TestRouterCommands(t *testing.T) {
	tests := []struct {
		name      string
		setup     []string // команды мастера перед тестом
		userID    int64
		text      string
		wantReply string // подстрока ответа
		wantTo    []int64
	}{
		{"start for client", nil, clientID, "/start", "/slots", nil},
		{"admin help has admin commands", nil, adminID, "/help", "/add", nil},
		{"not a command", nil, clientID, "привет", "только команды", nil},
		{"unknown command", nil, clientID, "/foo", "Неизвестная команда", nil},

		{"admin adds slot", nil, adminID, "/add 12.10 14:00", "Окно #1 добавлено: пн 12.10 14:00", nil},
		{"add bad format", nil, adminID, "/add завтра", "Формат:", nil},
		{"add in past", nil, adminID, "/add 05.10 09:00", "уже прошло", nil},
		{"client cannot add", nil, clientID, "/add 12.10 14:00", "только мастеру", nil},

		{"empty slots", nil, clientID, "/slots", "Свободных окон пока нет", nil},
		{"list slots", []string{"/add 12.10 14:00"}, clientID, "/slots", "#1 — пн 12.10 14:00", nil},

		{"book notifies admin", []string{"/add 12.10 14:00"}, clientID, "/book 1", "Вы записаны на пн 12.10 14:00", []int64{adminID}},
		{"book without id", nil, clientID, "/book", "Укажите номер окна", nil},
		{"book unknown", nil, clientID, "/book 9", "не найдено", nil},
		{"my without bookings", nil, clientID, "/my", "нет записей", nil},
		{"schedule only for admin", nil, clientID, "/schedule", "только мастеру", nil},
		{"empty schedule", nil, adminID, "/schedule", "Расписание пустое", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := newRouter(t)
			for _, cmd := range tt.setup {
				say(r, adminID, cmd)
			}
			resp := say(r, tt.userID, tt.text)

			if !strings.Contains(resp.Reply, tt.wantReply) {
				t.Errorf("reply %q does not contain %q", resp.Reply, tt.wantReply)
			}
			if got := recipients(resp); !equalIDs(got, tt.wantTo) {
				t.Errorf("notified %v, want %v", got, tt.wantTo)
			}
		})
	}
}

func TestClientHelpHidesAdminCommands(t *testing.T) {
	resp := say(newRouter(t), clientID, "/help")
	if strings.Contains(resp.Reply, "/add") {
		t.Errorf("client sees admin commands: %q", resp.Reply)
	}
}

// Полный сценарий: мастер добавляет окно, клиент записывается,
// второй клиент не может занять то же окно, затем отмены с уведомлениями.
func TestRouterBookingFlow(t *testing.T) {
	r := newRouter(t)
	steps := []struct {
		userID    int64
		text      string
		wantReply string
		wantTo    []int64
	}{
		{adminID, "/add 12.10 14:00", "Окно #1 добавлено", nil},
		{clientID, "/book 1", "Вы записаны", []int64{adminID}},
		{otherID, "/book 1", "уже занято", nil},
		{otherID, "/slots", "Свободных окон пока нет", nil},
		{clientID, "/my", "#1 — пн 12.10 14:00", nil},
		{adminID, "/schedule", "#1 — пн 12.10 14:00 — Анна (id 100)", nil},
		{otherID, "/cancel 1", "не найдено", nil}, // чужую запись не видно
		{clientID, "/cancel 1", "отменена", []int64{adminID}},
		{clientID, "/cancel 1", "никто не записан", nil},
		{otherID, "/book 1", "Вы записаны", []int64{adminID}},
		{adminID, "/cancel 1", "отменена", []int64{otherID}}, // мастер отменил — уведомляем клиента
		{adminID, "/schedule", "свободно", nil},
	}
	for i, s := range steps {
		resp := say(r, s.userID, s.text)
		if !strings.Contains(resp.Reply, s.wantReply) {
			t.Fatalf("step %d %q: reply %q does not contain %q", i, s.text, resp.Reply, s.wantReply)
		}
		if got := recipients(resp); !equalIDs(got, s.wantTo) {
			t.Fatalf("step %d %q: notified %v, want %v", i, s.text, got, s.wantTo)
		}
	}
}

func recipients(resp Response) []int64 {
	var ids []int64
	for _, n := range resp.Notify {
		ids = append(ids, n.ChatID)
	}
	return ids
}

func equalIDs(a, b []int64) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
