package booking_test

import (
	"context"
	"errors"
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

// "Сейчас" в тестах всегда одно и то же — тесты не зависят от реальной даты.
var now = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func fixedNow() time.Time { return now }

// fixture: сервис с тремя окнами — #1 свободно, #2 занято clientID, #3 свободно.
func newService(t *testing.T) *booking.Service {
	t.Helper()
	ctx := context.Background()
	svc := booking.NewService(memory.New(), adminID, fixedNow)
	for _, h := range []time.Duration{1, 2, 3} {
		if _, err := svc.AddSlot(ctx, adminID, now.Add(h*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := svc.Book(ctx, 2, booking.Client{ID: clientID, Name: "Анна"}); err != nil {
		t.Fatal(err)
	}
	return svc
}

func TestAddSlot(t *testing.T) {
	tests := []struct {
		name    string
		userID  int64
		start   time.Time
		wantErr error
	}{
		{"admin adds future slot", adminID, now.Add(24 * time.Hour), nil},
		{"client cannot add", clientID, now.Add(24 * time.Hour), booking.ErrForbidden},
		{"past time", adminID, now.Add(-time.Minute), booking.ErrInPast},
		{"exactly now is past", adminID, now, booking.ErrInPast},
		{"duplicate time", adminID, now.Add(time.Hour), booking.ErrSlotExists},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newService(t)
			slot, err := svc.AddSlot(context.Background(), tt.userID, tt.start)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && (slot.ID == 0 || !slot.Start.Equal(tt.start) || !slot.IsFree()) {
				t.Errorf("unexpected slot %+v", slot)
			}
		})
	}
}

func TestBook(t *testing.T) {
	tests := []struct {
		name    string
		slotID  int64
		client  int64
		wantErr error
	}{
		{"free slot", 1, otherID, nil},
		{"taken slot", 2, otherID, booking.ErrSlotTaken},
		{"unknown slot", 99, otherID, booking.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newService(t)
			slot, err := svc.Book(context.Background(), tt.slotID, booking.Client{ID: tt.client})
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err == nil && (slot.Client == nil || slot.Client.ID != tt.client) {
				t.Errorf("slot not booked by client: %+v", slot)
			}
		})
	}
}

func TestBookPastSlot(t *testing.T) {
	ctx := context.Background()
	repo := memory.New()
	past, _ := repo.Create(ctx, now.Add(-time.Hour)) // в обход сервиса: сервис такое создать не даст
	svc := booking.NewService(repo, adminID, fixedNow)

	if _, err := svc.Book(ctx, past.ID, booking.Client{ID: clientID}); !errors.Is(err, booking.ErrInPast) {
		t.Fatalf("err = %v, want ErrInPast", err)
	}
}

func TestCancel(t *testing.T) {
	tests := []struct {
		name    string
		userID  int64
		slotID  int64
		wantErr error
	}{
		{"client cancels own booking", clientID, 2, nil},
		{"admin cancels any booking", adminID, 2, nil},
		{"other client cannot cancel", otherID, 2, booking.ErrNotFound},
		{"free slot", clientID, 1, booking.ErrSlotNotTaken},
		{"unknown slot", clientID, 99, booking.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := newService(t)
			ctx := context.Background()
			_, err := svc.Cancel(ctx, tt.userID, tt.slotID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err = %v, want %v", err, tt.wantErr)
			}
			if err != nil {
				return
			}
			// После отмены окно снова свободно и его можно занять.
			if _, err := svc.Book(ctx, tt.slotID, booking.Client{ID: otherID}); err != nil {
				t.Errorf("slot not released: %v", err)
			}
		})
	}
}

func TestLists(t *testing.T) {
	ctx := context.Background()
	svc := newService(t)

	ids := func(slots []booking.Slot, err error) []int64 {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
		res := []int64{}
		for _, s := range slots {
			res = append(res, s.ID)
		}
		return res
	}

	tests := []struct {
		name string
		got  []int64
		want []int64
	}{
		{"free slots", ids(svc.FreeSlots(ctx)), []int64{1, 3}},
		{"client bookings", ids(svc.ClientBookings(ctx, clientID)), []int64{2}},
		{"other client has none", ids(svc.ClientBookings(ctx, otherID)), []int64{}},
		{"admin schedule sorted by time", ids(svc.Schedule(ctx, adminID)), []int64{1, 2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !equal(tt.got, tt.want) {
				t.Errorf("got %v, want %v", tt.got, tt.want)
			}
		})
	}

	if _, err := svc.Schedule(ctx, clientID); !errors.Is(err, booking.ErrForbidden) {
		t.Errorf("client schedule: err = %v, want ErrForbidden", err)
	}
}

func equal(a, b []int64) bool {
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
