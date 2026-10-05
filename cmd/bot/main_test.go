package main

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// До Telegram дело не доходит: проверяем, что run падает с понятной ошибкой
// на неверной конфигурации и недоступной базе.
func TestRunStartupErrors(t *testing.T) {
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o644); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		env     map[string]string
		wantErr string
	}{
		{"empty config", map[string]string{}, "load config"},
		{"bad db path", map[string]string{"BOT_TOKEN": "1:x", "ADMIN_ID": "1", "DB_PATH": filepath.Join(file, "bot.db")}, "open storage"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			getenv := func(k string) string { return tt.env[k] }
			err := run(context.Background(), getenv, io.Discard)
			if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("err = %v, want %q", err, tt.wantErr)
			}
		})
	}
}
