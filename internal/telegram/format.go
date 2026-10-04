package telegram

import (
	"fmt"
	"strings"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"
)

// Индекс совпадает с time.Weekday: 0 — воскресенье.
var weekdays = [...]string{"вс", "пн", "вт", "ср", "чт", "пт", "сб"}

func formatTime(t time.Time, loc *time.Location) string {
	t = t.In(loc)
	return fmt.Sprintf("%s %s", weekdays[t.Weekday()], t.Format("02.01 15:04"))
}

func formatClient(c booking.Client) string {
	return fmt.Sprintf("%s (id %d)", c.Name, c.ID)
}

// formatSlots выводит список окон; withClients — показывать ли, кто записан (для мастера).
func formatSlots(title string, slots []booking.Slot, loc *time.Location, withClients bool) string {
	var b strings.Builder
	b.WriteString(title)
	for _, s := range slots {
		fmt.Fprintf(&b, "\n#%d — %s", s.ID, formatTime(s.Start, loc))
		if withClients {
			if s.IsFree() {
				b.WriteString(" — свободно")
			} else {
				b.WriteString(" — " + formatClient(*s.Client))
			}
		}
	}
	return b.String()
}

const clientHelp = `Команды:
/slots — свободные окна
/book N — записаться на окно N
/my — мои записи
/cancel N — отменить запись N
/help — эта справка`

const adminHelp = clientHelp + `

Для мастера:
/add ДД.ММ ЧЧ:ММ — добавить окно (можно ДД.ММ.ГГГГ)
/schedule — всё расписание с клиентами
/cancel N — отменить любую запись`
