package telegram

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"

	"github.com/TaRosh/tgBotRecord/internal/booking"
	"github.com/TaRosh/tgBotRecord/internal/storage/memory"
)

const testToken = "123:TEST"

type sentMessage struct {
	ChatID int64
	Text   string
}

// fakeTelegram — фальшивый api.telegram.org. Он делает то же, что настоящий:
// отвечает на getMe, отдаёт входящие сообщения через getUpdates (long polling)
// и принимает исходящие sendMessage, складывая их в канал для проверки.
func fakeTelegram(t *testing.T, updates string) (url string, sent <-chan sentMessage) {
	t.Helper()
	out := make(chan sentMessage, 10)
	var delivered atomic.Bool

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := strings.TrimPrefix(r.URL.Path, "/bot"+testToken+"/")
		switch method {
		case "getMe":
			fmt.Fprint(w, `{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"TestBot"}}`)
		case "getUpdates":
			if delivered.CompareAndSwap(false, true) {
				fmt.Fprintf(w, `{"ok":true,"result":%s}`, updates)
				return
			}
			// Новых сообщений нет: настоящий сервер держал бы запрос до таймаута.
			select {
			case <-r.Context().Done():
			case <-time.After(50 * time.Millisecond):
			}
			fmt.Fprint(w, `{"ok":true,"result":[]}`)
		case "sendMessage":
			// Библиотека отправляет параметры как multipart/form-data.
			chatID, _ := strconv.ParseInt(r.FormValue("chat_id"), 10, 64)
			out <- sentMessage{ChatID: chatID, Text: r.FormValue("text")}
			fmt.Fprint(w, `{"ok":true,"result":{"message_id":1,"date":0,"chat":{"id":1,"type":"private"}}}`)
		default:
			t.Errorf("unexpected API method %q", method)
			http.Error(w, "unknown method", http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv.URL, out
}

func message(updateID int, chatType models.ChatType, userID int64, text string) string {
	return fmt.Sprintf(`{"update_id":%d,"message":{"message_id":%d,"date":0,`+
		`"chat":{"id":%d,"type":%q},"from":{"id":%d,"is_bot":false,"first_name":"Анна","username":"anna"},"text":%q}}`,
		updateID, updateID, userID, chatType, userID, text)
}

func TestRunEndToEnd(t *testing.T) {
	// Окно #1 через 1 час, чтобы клиент мог на него записаться.
	svc := booking.NewService(memory.New(), adminID, time.Now)
	if _, err := svc.AddSlot(context.Background(), adminID, time.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	router := NewRouter(svc, adminID, time.UTC, time.Now, log)

	updates := "[" + message(1, models.ChatTypeGroup, -500, "/slots") + "," + // из группы — игнорируем
		message(2, models.ChatTypePrivate, clientID, "/book 1") + "]"
	url, sent := fakeTelegram(t, updates)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- Run(ctx, testToken, router, log, bot.WithServerURL(url)) }()

	// Ждём ровно два сообщения: ответ клиенту и уведомление мастеру (порядок не важен).
	got := map[int64]string{}
	for len(got) < 2 {
		select {
		case m := <-sent:
			got[m.ChatID] = m.Text
		case <-time.After(5 * time.Second):
			t.Fatalf("timeout, received so far: %v", got)
		}
	}
	if !strings.Contains(got[clientID], "Вы записаны") {
		t.Errorf("client reply = %q", got[clientID])
	}
	if !strings.Contains(got[adminID], "Анна @anna") {
		t.Errorf("admin notification = %q", got[adminID])
	}

	// Сообщение из группы не должно породить ответ.
	select {
	case m := <-sent:
		t.Errorf("unexpected message: %+v", m)
	case <-time.After(200 * time.Millisecond):
	}

	// Отмена контекста (как SIGTERM) должна корректно остановить бота.
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run returned %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not stop after context cancel")
	}
}

func TestRunInvalidToken(t *testing.T) {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	if err := Run(context.Background(), " ", nil, log); err == nil {
		t.Fatal("expected error for empty token")
	}
}

func TestDisplayName(t *testing.T) {
	tests := []struct {
		name string
		user models.User
		want string
	}{
		{"full", models.User{FirstName: "Анна", LastName: "Иванова", Username: "anna"}, "Анна Иванова @anna"},
		{"first name only", models.User{FirstName: "Анна"}, "Анна"},
		{"username only", models.User{Username: "anna"}, "@anna"},
		{"empty", models.User{}, "без имени"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := displayName(&tt.user); got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}
