package telegram

import (
	"errors"
	"strconv"
	"strings"
	"time"
)

var errBadFormat = errors.New("bad format")

// parseCommand разбирает "/book@MyBot 3" на команду "book" и аргументы "3".
// ok == false, если текст не является командой.
func parseCommand(text string) (cmd, args string, ok bool) {
	text = strings.TrimSpace(text)
	if !strings.HasPrefix(text, "/") {
		return "", "", false
	}
	head, args, _ := strings.Cut(text[1:], " ")
	// В группах Telegram добавляет к команде имя бота: /help@MyBot.
	cmd, _, _ = strings.Cut(head, "@")
	if cmd == "" {
		return "", "", false
	}
	return strings.ToLower(cmd), strings.TrimSpace(args), true
}

// parseID разбирает номер окна: "3" или "#3".
func parseID(s string) (int64, error) {
	id, err := strconv.ParseInt(strings.TrimPrefix(strings.TrimSpace(s), "#"), 10, 64)
	if err != nil || id <= 0 {
		return 0, errBadFormat
	}
	return id, nil
}

// Форматы "2" и "1" принимают день и месяц как с ведущим нулём, так и без него.
const (
	layoutWithYear = "2.1.2006 15:04"
	layoutNoYear   = "2.1"
)

// parseSlotTime разбирает "12.10 14:00" или "12.10.2026 14:00" в часовом поясе loc.
// Если год не указан, берётся ближайшая такая дата начиная с сегодняшнего дня.
func parseSlotTime(s string, now time.Time, loc *time.Location) (time.Time, error) {
	datePart, timePart, ok := strings.Cut(strings.Join(strings.Fields(s), " "), " ")
	if !ok {
		return time.Time{}, errBadFormat
	}

	if strings.Count(datePart, ".") == 2 {
		t, err := time.ParseInLocation(layoutWithYear, datePart+" "+timePart, loc)
		if err != nil {
			return time.Time{}, errBadFormat
		}
		return t, nil
	}

	// Год подставляем строкой и парсим целиком, а не через time.Date:
	// time.Date молча превратит 29.02.2027 в 01.03.2027, а Parse вернёт ошибку.
	if _, err := time.Parse(layoutNoYear, datePart); err != nil {
		return time.Time{}, errBadFormat
	}
	now = now.In(loc)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	for _, year := range []int{now.Year(), now.Year() + 1} {
		t, err := time.ParseInLocation(layoutWithYear,
			datePart+"."+strconv.Itoa(year)+" "+timePart, loc)
		if err != nil {
			continue // например, 29.02 в невисокосном году — пробуем следующий
		}
		// Сравниваем по дню, а не по времени: "сегодня 09:00", когда уже 12:00,
		// должно дать понятную ошибку "в прошлом", а не окно через год.
		if !t.Before(today) {
			return t, nil
		}
	}
	return time.Time{}, errBadFormat
}
