package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	_ "embed"
	_ "github.com/mattn/go-sqlite3"
)

//go:embed schema.sql
var schemaSQL string

var defaultCategories = []string{
	"General",
	"Tech",
	"Science",
	"Art",
	"Sports",
	"Music",
	"News",
}

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite3", path)
	if err != nil {
		return nil, err
	}

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, err
	}

	if _, err := db.Exec("PRAGMA foreign_keys = ON"); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := applySchema(db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureCommentParentColumn(db); err != nil {
		_ = db.Close()
		return nil, err
	}

	if err := seedCategories(context.Background(), db, defaultCategories); err != nil {
		_ = db.Close()
		return nil, err
	}

	return db, nil
}

func applySchema(db *sql.DB) error {
	schema := strings.TrimSpace(schemaSQL)
	if schema == "" {
		return fmt.Errorf("schema is empty")
	}
	_, err := db.Exec(schema)
	return err
}

func ensureCommentParentColumn(db *sql.DB) error {
	rows, err := db.Query("PRAGMA table_info(comments)")
	if err != nil {
		return err
	}
	defer rows.Close()

	var (
		cid       int
		name      string
		ctype     string
		notnull   int
		dfltValue sql.NullString
		pk        int
	)
	for rows.Next() {
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return err
		}
		if strings.EqualFold(name, "parent_id") {
			return rows.Err()
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}

	_, err = db.Exec("ALTER TABLE comments ADD COLUMN parent_id INTEGER")
	return err
}

func seedCategories(ctx context.Context, db *sql.DB, categories []string) error {
	for _, name := range categories {
		if strings.TrimSpace(name) == "" {
			continue
		}
		if _, err := db.ExecContext(ctx, "INSERT OR IGNORE INTO categories(name) VALUES (?)", name); err != nil {
			return err
		}
	}
	return nil
}
