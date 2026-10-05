// Package reminder отправляет клиентам напоминания о записи.
package reminder

import (
	"container/heap"
	"time"
)

// item — "будильник": в момент At проверить окно SlotID и напомнить.
type item struct {
	At     time.Time
	SlotID int64
}

// queue — очередь с приоритетом на двоичной куче (min-heap): на вершине
// (индекс 0) всегда ближайший по времени будильник.
//
// Почему куча, а не отсортированный срез:
//   - добавить элемент:  куча O(log n), срез O(n) — нужно сдвигать хвост;
//   - узнать ближайший:  O(1) в обоих случаях;
//   - извлечь ближайший: куча O(log n), срез O(n) при удалении из начала.
//
// Куча хранится в обычном срезе: у элемента i потомки — 2i+1 и 2i+2,
// и каждый родитель не позже своих потомков. Полностью отсортированной
// она при этом не бывает — порядок гарантирован только для вершины.
//
// Методы ниже — интерфейс heap.Interface. Вызывать их напрямую нельзя:
// только через heap.Push / heap.Pop, которые поддерживают свойство кучи.
type queue []item

func (q queue) Len() int           { return len(q) }
func (q queue) Less(i, j int) bool { return q[i].At.Before(q[j].At) }
func (q queue) Swap(i, j int)      { q[i], q[j] = q[j], q[i] }

func (q *queue) Push(x any) { *q = append(*q, x.(item)) }

func (q *queue) Pop() any {
	old := *q
	n := len(old)
	it := old[n-1]
	*q = old[:n-1]
	return it
}

// peek возвращает ближайший будильник, не извлекая его.
func (q queue) peek() (item, bool) {
	if len(q) == 0 {
		return item{}, false
	}
	return q[0], true
}

// popDue извлекает все будильники, время которых наступило к моменту now.
func (q *queue) popDue(now time.Time) []item {
	var due []item
	for q.Len() > 0 && !(*q)[0].At.After(now) {
		due = append(due, heap.Pop(q).(item))
	}
	return due
}
