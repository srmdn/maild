package migrate

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func TestLegacyAPIKeyWorkspaceMigration(t *testing.T) {
	dsn := os.Getenv("MAILD_HTTP_TEST_DSN")
	if dsn == "" {
		t.Skip("dedicated MAILD_HTTP_TEST_DSN required")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	// Pin the connection so the private schema's search_path applies throughout.
	db.SetMaxOpenConns(1)
	ctx := context.Background()
	schema := fmt.Sprintf("auth_migration_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, "CREATE SCHEMA "+schema); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = db.ExecContext(ctx, "DROP SCHEMA "+schema+" CASCADE") }()
	if _, err := db.ExecContext(ctx, "SET search_path TO "+schema); err != nil {
		t.Fatal(err)
	}
	entries, err := migrationsFS.ReadDir("sql")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".up.sql") || strings.HasPrefix(entry.Name(), "000011_") {
			continue
		}
		data, err := migrationsFS.ReadFile("sql/" + entry.Name())
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, string(data)); err != nil {
			t.Fatalf("legacy migration %s: %v", entry.Name(), err)
		}
	}
	_, err = db.ExecContext(ctx, `INSERT INTO users (id,email,password_hash) VALUES (1,'member@example.test','unused'),(2,'orphan@example.test','unused');
 INSERT INTO workspaces (id,name) VALUES (11,'first'),(12,'second');
 INSERT INTO user_workspaces (user_id,workspace_id,created_at) VALUES (1,12,'2020-01-02'),(1,11,'2020-01-01');
 INSERT INTO user_api_keys (user_id,name,key_hash) VALUES (1,'legacy','legacyhash'),(2,'orphan','orphanhash');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := Up(ctx, db); err != nil {
		t.Fatal(err)
	}
	var workspace sql.NullInt64
	var prefix, hash string
	if err := db.QueryRowContext(ctx, "SELECT workspace_id,key_prefix,key_hash FROM user_api_keys WHERE user_id=1").Scan(&workspace, &prefix, &hash); err != nil {
		t.Fatal(err)
	}
	if !workspace.Valid || workspace.Int64 != 11 || prefix != "" || hash != "legacyhash" {
		t.Fatalf("legacy key changed incorrectly: ws=%v prefix=%q hash=%q", workspace, prefix, hash)
	}
	if err := db.QueryRowContext(ctx, "SELECT workspace_id FROM user_api_keys WHERE user_id=2").Scan(&workspace); err != nil {
		t.Fatal(err)
	}
	if workspace.Valid {
		t.Fatal("orphan key was granted a workspace")
	}
	if err := Up(ctx, db); err != nil {
		t.Fatalf("second migration run: %v", err)
	}
}
