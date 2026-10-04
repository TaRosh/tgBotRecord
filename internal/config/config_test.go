package config

import (
	"log/slog"
	"strings"
	"testing"
)

// envFrom превращает map в функцию-аналог os.Getenv.
func envFrom(m map[string]string) func(string) string {
	return func(key string) string { return m[key] }
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		want    Config // проверяем только при wantErr == ""
		wantTZ  string
		wantErr []string // подстроки, которые должны быть в тексте ошибки
	}{
		{
			name: "all values set",
			env: map[string]string{
				"BOT_TOKEN": "123:abc",
				"ADMIN_ID":  "42",
				"TIMEZONE":  "Asia/Yekaterinburg",
				"DB_PATH":   "/tmp/test.db",
				"LOG_LEVEL": "debug",
			},
			want:   Config{BotToken: "123:abc", AdminID: 42, DBPath: "/tmp/test.db", LogLevel: slog.LevelDebug},
			wantTZ: "Asia/Yekaterinburg",
		},
		{
			name:   "defaults applied",
			env:    map[string]string{"BOT_TOKEN": "123:abc", "ADMIN_ID": "42"},
			want:   Config{BotToken: "123:abc", AdminID: 42, DBPath: defaultDBPath, LogLevel: slog.LevelInfo},
			wantTZ: defaultTimezone,
		},
		{
			name:   "values are trimmed",
			env:    map[string]string{"BOT_TOKEN": "  123:abc ", "ADMIN_ID": " 42 ", "LOG_LEVEL": " WARN "},
			want:   Config{BotToken: "123:abc", AdminID: 42, DBPath: defaultDBPath, LogLevel: slog.LevelWarn},
			wantTZ: defaultTimezone,
		},
		{
			name:    "empty env reports all required fields",
			env:     map[string]string{},
			wantErr: []string{"BOT_TOKEN is required", "ADMIN_ID is required"},
		},
		{
			name:    "admin id not a number",
			env:     map[string]string{"BOT_TOKEN": "t", "ADMIN_ID": "abc"},
			wantErr: []string{"ADMIN_ID must be a positive integer"},
		},
		{
			name:    "admin id negative",
			env:     map[string]string{"BOT_TOKEN": "t", "ADMIN_ID": "-5"},
			wantErr: []string{"ADMIN_ID must be a positive integer"},
		},
		{
			name:    "unknown timezone",
			env:     map[string]string{"BOT_TOKEN": "t", "ADMIN_ID": "1", "TIMEZONE": "Mars/Olympus"},
			wantErr: []string{"unknown time zone"},
		},
		{
			name:    "unknown log level",
			env:     map[string]string{"BOT_TOKEN": "t", "ADMIN_ID": "1", "LOG_LEVEL": "verbose"},
			wantErr: []string{"LOG_LEVEL"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Load(envFrom(tt.env))

			if len(tt.wantErr) > 0 {
				if err == nil {
					t.Fatalf("expected error containing %q, got nil", tt.wantErr)
				}
				for _, sub := range tt.wantErr {
					if !strings.Contains(err.Error(), sub) {
						t.Errorf("error %q does not contain %q", err, sub)
					}
				}
				return
			}

			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got.BotToken != tt.want.BotToken || got.AdminID != tt.want.AdminID ||
				got.DBPath != tt.want.DBPath || got.LogLevel != tt.want.LogLevel {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
			if got.Location == nil || got.Location.String() != tt.wantTZ {
				t.Errorf("timezone = %v, want %s", got.Location, tt.wantTZ)
			}
		})
	}
}
