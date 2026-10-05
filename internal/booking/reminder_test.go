package booking_test

import (
	"context"
	"testing"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
	"github.com/TaRosh/tgBotRecord/internal/storage/memory"
)

const remindBefore = 24 * time.Hour

// Правило напоминаний на фиксированном "сейчас" (5 октября, 12:00).
func TestPendingAndClaimReminder(t *testing.T) {
	tests := []struct {
		name     string
		startIn  time.Duration // начало окна относительно now
		bookedIn time.Duration // когда клиент записался относительно now
		booked   bool
		reminded bool
		want     bool
	}{
		{"booked long ago, reminder in future", 48 * time.Hour, -72 * time.Hour, true, false, true},
		{"reminder moment passed while bot was down", 2 * time.Hour, -72 * time.Hour, true, false, true},
		{"booked inside window", 2 * time.Hour, -1 * time.Hour, true, false, false},
		{"booked exactly at reminder moment", 2 * time.Hour, -22 * time.Hour, true, false, false},
		{"already reminded", 48 * time.Hour, -72 * time.Hour, true, true, false},
		{"free slot", 48 * time.Hour, 0, false, false, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := context.Background()
			repo := memory.New()
			svc := booking.NewService(repo, adminID, fixedNow)

			slot, err := repo.Create(ctx, now.Add(tt.startIn))
			if err != nil {
				t.Fatal(err)
			}
			if tt.booked {
				if err := repo.Book(ctx, slot.ID, booking.Client{ID: clientID}, now.Add(tt.bookedIn)); err != nil {
					t.Fatal(err)
				}
			}
			if tt.reminded {
				if _, err := repo.MarkReminded(ctx, slot.ID, clientID); err != nil {
					t.Fatal(err)
				}
			}

			pending, err := svc.PendingReminders(ctx, remindBefore)
			if err != nil {
				t.Fatal(err)
			}
			if got := len(pending) == 1; got != tt.want {
				t.Errorf("pending = %v, want %v", got, tt.want)
			}

			_, ok, err := svc.ClaimReminder(ctx, slot.ID, remindBefore)
			if err != nil {
				t.Fatal(err)
			}
			if ok != tt.want {
				t.Errorf("claim = %v, want %v", ok, tt.want)
			}
			// Повторный claim никогда не проходит: защита от дублей.
			if _, ok, _ := svc.ClaimReminder(ctx, slot.ID, remindBefore); ok {
				t.Error("second claim must fail")
			}
		})
	}
}

func TestClaimReminderEdgeCases(t *testing.T) {
	ctx := context.Background()
	svc := newService(t) // окно #2 занято clientID; записан "сейчас"

	tests := []struct {
		name   string
		slotID int64
		before time.Duration
	}{
		{"unknown slot", 99, time.Minute},
		{"started slot", 2, 10 * time.Hour}, // напоминание за 10 ч, а визит через 2 ч: записался внутри окна
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, ok, err := svc.ClaimReminder(ctx, tt.slotID, tt.before); ok || err != nil {
				t.Errorf("got ok=%v err=%v, want false, nil", ok, err)
			}
		})
	}
}

func TestOnBookedHook(t *testing.T) {
	ctx := context.Background()
	svc := booking.NewService(memory.New(), adminID, fixedNow)
	slot, err := svc.AddSlot(ctx, adminID, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}

	var got []booking.Slot
	svc.OnBooked(func(s booking.Slot) { got = append(got, s) })

	if _, err := svc.Book(ctx, slot.ID, booking.Client{ID: clientID}); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Book(ctx, slot.ID, booking.Client{ID: otherID}); err == nil {
		t.Fatal("second booking must fail")
	}

	if len(got) != 1 {
		t.Fatalf("hook called %d times, want 1 (only for successful booking)", len(got))
	}
	if got[0].ID != slot.ID || got[0].Client.ID != clientID || !got[0].BookedAt.Equal(now) {
		t.Errorf("hook got %+v", got[0])
	}
}
