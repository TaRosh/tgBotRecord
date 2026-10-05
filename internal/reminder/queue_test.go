package reminder

import (
	"container/heap"
	"math/rand/v2"
	"slices"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 5, 12, 0, 0, 0, time.UTC)

func at(minutes int) time.Time { return t0.Add(time.Duration(minutes) * time.Minute) }

func TestQueueOrder(t *testing.T) {
	tests := []struct {
		name    string
		minutes []int // порядок добавления
	}{
		{"empty", nil},
		{"single", []int{5}},
		{"already sorted", []int{1, 2, 3, 4}},
		{"reversed", []int{9, 7, 5, 3, 1}},
		{"mixed with duplicates", []int{30, 10, 20, 10, 0, 25}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var q queue
			for i, m := range tt.minutes {
				heap.Push(&q, item{At: at(m), SlotID: int64(i)})
			}

			var got []int
			for q.Len() > 0 {
				it := heap.Pop(&q).(item)
				got = append(got, int(it.At.Sub(t0).Minutes()))
			}

			want := slices.Clone(tt.minutes)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("pop order %v, want %v", got, want)
			}
		})
	}
}

// Свойство кучи на случайных данных: сколько бы элементов ни добавили,
// извлекаются они всегда по возрастанию времени.
func TestQueueRandom(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2)) // фиксированное зерно — тест воспроизводим
	var q queue
	for range 1000 {
		heap.Push(&q, item{At: at(r.IntN(10_000))})
	}
	prev := time.Time{}
	for q.Len() > 0 {
		it := heap.Pop(&q).(item)
		if it.At.Before(prev) {
			t.Fatalf("heap order broken: %v after %v", it.At, prev)
		}
		prev = it.At
	}
}

func TestQueuePeekAndPopDue(t *testing.T) {
	var q queue
	if _, ok := q.peek(); ok {
		t.Fatal("peek on empty queue must return ok=false")
	}
	for i, m := range []int{30, 10, 20} {
		heap.Push(&q, item{At: at(m), SlotID: int64(i + 1)})
	}

	if top, _ := q.peek(); !top.At.Equal(at(10)) {
		t.Fatalf("peek = %v, want %v", top.At, at(10))
	}

	tests := []struct {
		now     int
		wantIDs []int64
		wantLen int
	}{
		{now: 5, wantIDs: nil, wantLen: 3},            // ещё ничего не наступило
		{now: 20, wantIDs: []int64{2, 3}, wantLen: 1}, // граница включительно
		{now: 100, wantIDs: []int64{1}, wantLen: 0},
	}
	for _, tt := range tests {
		var ids []int64
		for _, it := range q.popDue(at(tt.now)) {
			ids = append(ids, it.SlotID)
		}
		if !slices.Equal(ids, tt.wantIDs) || q.Len() != tt.wantLen {
			t.Errorf("popDue(%d) = %v (left %d), want %v (left %d)", tt.now, ids, q.Len(), tt.wantIDs, tt.wantLen)
		}
	}
}
