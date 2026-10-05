// Package sqlite — хранилище окон в файле SQLite.
// Драйвер modernc.org/sqlite написан на чистом Go: не нужен cgo и компилятор C,
// поэтому бинарник собирается в маленький Docker-образ без лишних библиотек.
package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/TaRosh/tgBotRecord/internal/booking"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// Время храним как Unix-секунды в UTC: целое число однозначно,
// не зависит от часового пояса и правильно сортируется.
// UNIQUE(start_at) автоматически создаёт индекс (B-дерево), поэтому
// поиск "окна начиная с даты" с сортировкой не читает всю таблицу.
const schema = `
CREATE TABLE IF NOT EXISTS slots (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	start_at    INTEGER NOT NULL UNIQUE,
	client_id   INTEGER,
	client_name TEXT
);`

// Repo реализует booking.Repository.
type Repo struct {
	db *sql.DB
}

// Open открывает (или создаёт) базу по пути path и применяет схему.
func Open(ctx context.Context, path string) (*Repo, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create db dir: %w", err)
	}

	// Параметры _pragma применяются к КАЖДОМУ соединению из пула database/sql:
	//   journal_mode(WAL)  — чтение не блокирует запись;
	//   busy_timeout(5000) — при занятой базе ждать до 5 с, а не сразу падать с SQLITE_BUSY.
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open db: %w", err)
	}
	// sql.Open не подключается сразу — проверяем соединение и создаём таблицу.
	if _, err := db.ExecContext(ctx, schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Repo{db: db}, nil
}

func (r *Repo) Close() error { return r.db.Close() }

func (r *Repo) Create(ctx context.Context, start time.Time) (booking.Slot, error) {
	res, err := r.db.ExecContext(ctx, `INSERT INTO slots (start_at) VALUES (?)`, start.Unix())
	if err != nil {
		if isUniqueViolation(err) {
			return booking.Slot{}, booking.ErrSlotExists
		}
		return booking.Slot{}, fmt.Errorf("insert slot: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return booking.Slot{}, fmt.Errorf("last insert id: %w", err)
	}
	return booking.Slot{ID: id, Start: fromUnix(start.Unix())}, nil
}

func (r *Repo) Get(ctx context.Context, id int64) (booking.Slot, error) {
	row := r.db.QueryRowContext(ctx,
		`SELECT id, start_at, client_id, client_name FROM slots WHERE id = ?`, id)
	s, err := scanSlot(row)
	if errors.Is(err, sql.ErrNoRows) {
		return booking.Slot{}, booking.ErrNotFound
	}
	return s, err
}

func (r *Repo) Book(ctx context.Context, id int64, client booking.Client) error {
	// Атомарность обеспечивает сама база: условие "client_id IS NULL"
	// проверяется и запись выполняется одной командой. Из двух одновременных
	// UPDATE строку изменит только первый, второй получит 0 изменённых строк.
	res, err := r.db.ExecContext(ctx,
		`UPDATE slots SET client_id = ?, client_name = ? WHERE id = ? AND client_id IS NULL`,
		client.ID, client.Name, id)
	if err != nil {
		return fmt.Errorf("book slot: %w", err)
	}
	if n, err := res.RowsAffected(); err != nil {
		return fmt.Errorf("rows affected: %w", err)
	} else if n == 1 {
		return nil
	}
	// Ничего не обновили: окна нет или оно занято. Уточняем причину.
	if _, err := r.Get(ctx, id); err != nil {
		return err // ErrNotFound или ошибка базы
	}
	return booking.ErrSlotTaken
}

func (r *Repo) Release(ctx context.Context, id int64) error {
	res, err := r.db.ExecContext(ctx,
		`UPDATE slots SET client_id = NULL, client_name = NULL WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("release slot: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("rows affected: %w", err)
	}
	if n == 0 {
		return booking.ErrNotFound
	}
	return nil
}

func (r *Repo) ListFrom(ctx context.Context, from time.Time) ([]booking.Slot, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, start_at, client_id, client_name FROM slots
		 WHERE start_at >= ? ORDER BY start_at`, from.Unix())
	if err != nil {
		return nil, fmt.Errorf("list slots: %w", err)
	}
	defer rows.Close() // без Close соединение не вернётся в пул

	var slots []booking.Slot
	for rows.Next() {
		s, err := scanSlot(rows)
		if err != nil {
			return nil, err
		}
		slots = append(slots, s)
	}
	// rows.Next() возвращает false и при ошибке — её нужно проверить отдельно.
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate slots: %w", err)
	}
	return slots, nil
}

// scanner — общее у *sql.Row и *sql.Rows, чтобы не дублировать сканирование.
type scanner interface {
	Scan(dest ...any) error
}

func scanSlot(sc scanner) (booking.Slot, error) {
	var (
		s          booking.Slot
		startAt    int64
		clientID   sql.NullInt64 // NULL в базе — это "окно свободно"
		clientName sql.NullString
	)
	if err := sc.Scan(&s.ID, &startAt, &clientID, &clientName); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return booking.Slot{}, err
		}
		return booking.Slot{}, fmt.Errorf("scan slot: %w", err)
	}
	s.Start = fromUnix(startAt)
	if clientID.Valid {
		s.Client = &booking.Client{ID: clientID.Int64, Name: clientName.String}
	}
	return s, nil
}

func fromUnix(sec int64) time.Time { return time.Unix(sec, 0).UTC() }

func isUniqueViolation(err error) bool {
	var sqlErr *sqlite.Error
	return errors.As(err, &sqlErr) && sqlErr.Code() == sqlite3.SQLITE_CONSTRAINT_UNIQUE
}

var _ booking.Repository = (*Repo)(nil)
