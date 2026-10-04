package telegram

import (
	"testing"
	"time"
)

func TestParseCommand(t *testing.T) {
	tests := []struct {
		text     string
		wantCmd  string
		wantArgs string
		wantOK   bool
	}{
		{"/start", "start", "", true},
		{"/book 3", "book", "3", true},
		{"  /add   12.10 14:00  ", "add", "12.10 14:00", true},
		{"/help@MyRecordBot", "help", "", true},
		{"/BOOK 3", "book", "3", true},
		{"привет", "", "", false},
		{"/", "", "", false},
		{"", "", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.text, func(t *testing.T) {
			cmd, args, ok := parseCommand(tt.text)
			if cmd != tt.wantCmd || args != tt.wantArgs || ok != tt.wantOK {
				t.Errorf("got (%q, %q, %v), want (%q, %q, %v)", cmd, args, ok, tt.wantCmd, tt.wantArgs, tt.wantOK)
			}
		})
	}
}

func TestParseID(t *testing.T) {
	tests := []struct {
		in      string
		want    int64
		wantErr bool
	}{
		{"3", 3, false},
		{"#12", 12, false},
		{" 7 ", 7, false},
		{"", 0, true},
		{"0", 0, true},
		{"-1", 0, true},
		{"abc", 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.in, func(t *testing.T) {
			got, err := parseID(tt.in)
			if (err != nil) != tt.wantErr || got != tt.want {
				t.Errorf("parseID(%q) = %d, %v", tt.in, got, err)
			}
		})
	}
}

func TestParseSlotTime(t *testing.T) {
	msk, err := time.LoadLocation("Europe/Moscow")
	if err != nil {
		t.Fatal(err)
	}
	// "Сейчас": 5 октября 2026, 12:00 по Москве.
	now := time.Date(2026, 10, 5, 12, 0, 0, 0, msk)
	at := func(y int, m time.Month, d, h, min int) time.Time { return time.Date(y, m, d, h, min, 0, 0, msk) }

	tests := []struct {
		name    string
		now     time.Time
		in      string
		want    time.Time
		wantErr bool
	}{
		{"with year", now, "12.10.2026 14:00", at(2026, 10, 12, 14, 0), false},
		{"without year", now, "12.10 14:00", at(2026, 10, 12, 14, 0), false},
		{"no leading zeros", now, "1.11 9:05", at(2026, 11, 1, 9, 5), false},
		{"extra spaces", now, "  12.10    14:00 ", at(2026, 10, 12, 14, 0), false},
		{"date passed -> next year", now, "01.03 10:00", at(2027, 3, 1, 10, 0), false},
		// Сегодня, но время прошло: оставляем этот год, сервис ответит "время прошло".
		{"today earlier hour stays this year", now, "05.10 09:00", at(2026, 10, 5, 9, 0), false},
		{"leap day: no leap year within this and next", time.Date(2026, 3, 1, 0, 0, 0, 0, msk), "29.02 10:00", time.Time{}, true},
		{"leap day in leap year", time.Date(2027, 12, 1, 0, 0, 0, 0, msk), "29.02 10:00", at(2028, 2, 29, 10, 0), false},
		{"invalid date", now, "31.02 10:00", time.Time{}, true},
		{"invalid time", now, "12.10 25:00", time.Time{}, true},
		{"missing time", now, "12.10", time.Time{}, true},
		{"garbage", now, "завтра в обед", time.Time{}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSlotTime(tt.in, tt.now, msk)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if !got.Equal(tt.want) {
				t.Errorf("got %v, want %v", got, tt.want)
			}
		})
	}
}
