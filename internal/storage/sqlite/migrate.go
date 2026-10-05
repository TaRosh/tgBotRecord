package sqlite

import (
	"context"
	"database/sql"
	"fmt"
)

// migrations — история изменений схемы. Элемент i переводит базу из версии i
// в версию i+1. Старые миграции НИКОГДА не меняются: у заказчиков уже есть
// базы, созданные ими. Новое изменение — новый элемент в конце списка.
//
// Время храним как Unix-секунды в UTC: целое число однозначно,
// не зависит от часового пояса и правильно сортируется.
// UNIQUE(start_at) автоматически создаёт индекс (B-дерево), поэтому
// поиск "окна начиная с даты" с сортировкой не читает всю таблицу.
var migrations = []string{
	// v1: окна для записи.
	// IF NOT EXISTS — потому что базы до появления миграций уже содержат таблицу.
	`CREATE TABLE IF NOT EXISTS slots (
		id          INTEGER PRIMARY KEY AUTOINCREMENT,
		start_at    INTEGER NOT NULL UNIQUE,
		client_id   INTEGER,
		client_name TEXT
	)`,
	// v2: напоминания.
	`ALTER TABLE slots ADD COLUMN booked_at INTEGER;
	 ALTER TABLE slots ADD COLUMN reminded INTEGER NOT NULL DEFAULT 0`,
}

// migrate применяет недостающие миграции. Текущая версия схемы хранится
// в самом файле базы, в служебном поле SQLite user_version.
func migrate(ctx context.Context, db *sql.DB) error {
	var version int
	if err := db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}

	for i := version; i < len(migrations); i++ {
		// Каждая миграция — в своей транзакции: либо применится целиком
		// вместе с новым номером версии, либо не применится совсем.
		if err := applyMigration(ctx, db, i+1, migrations[i]); err != nil {
			return fmt.Errorf("migration v%d: %w", i+1, err)
		}
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, version int, stmt string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback() //nolint:errcheck // после Commit откат ничего не делает

	if _, err := tx.ExecContext(ctx, stmt); err != nil {
		return err
	}
	// PRAGMA не поддерживает параметры "?", поэтому число подставляем в текст.
	// Это безопасно: version — int из нашего кода, а не ввод пользователя.
	if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", version)); err != nil {
		return err
	}
	return tx.Commit()
}
