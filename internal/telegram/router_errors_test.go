package telegram

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
)

var errDB = errors.New("database is down")

// brokenRepo имитирует упавшую базу: любой метод возвращает ошибку.
type brokenRepo struct{}

func (brokenRepo) Create(context.Context, time.Time) (booking.Slot, error) {
	return booking.Slot{}, errDB
}
func (brokenRepo) Get(context.Context, int64) (booking.Slot, error)  { return booking.Slot{}, errDB }
func (brokenRepo) Book(context.Context, int64, booking.Client) error { return errDB }
func (brokenRepo) Release(context.Context, int64) error              { return errDB }
func (brokenRepo) ListFrom(context.Context, time.Time) ([]booking.Slot, error) {
	return nil, errDB
}

// При сбое базы пользователь видит вежливое сообщение без технических деталей,
// а подробности с исходной ошибкой попадают в лог для разработчика.
func TestRouterStorageFailure(t *testing.T) {
	tests := []struct {
		userID int64
		text   string
		wantOp string
	}{
		{clientID, "/slots", "free slots"},
		{clientID, "/my", "client bookings"},
		{clientID, "/book 1", "book"},
		{clientID, "/cancel 1", "cancel"},
		{adminID, "/add 12.10 14:00", "add"},
		{adminID, "/schedule", "schedule"},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			var logs bytes.Buffer
			now := func() time.Time { return time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC) }
			svc := booking.NewService(brokenRepo{}, adminID, now)
			r := NewRouter(svc, adminID, time.UTC, now, slog.New(slog.NewTextHandler(&logs, nil)))

			resp := say(r, tt.userID, tt.text)

			if !strings.Contains(resp.Reply, "Что-то пошло не так") {
				t.Errorf("reply = %q", resp.Reply)
			}
			if strings.Contains(resp.Reply, errDB.Error()) {
				t.Errorf("internal error leaked to user: %q", resp.Reply)
			}
			if len(resp.Notify) != 0 {
				t.Errorf("unexpected notifications: %v", resp.Notify)
			}
			if l := logs.String(); !strings.Contains(l, "op=\""+tt.wantOp+"\"") && !strings.Contains(l, "op="+tt.wantOp) {
				t.Errorf("log does not contain op %q: %s", tt.wantOp, l)
			}
			if !strings.Contains(logs.String(), errDB.Error()) {
				t.Errorf("log does not contain original error: %s", logs.String())
			}
		})
	}
}
