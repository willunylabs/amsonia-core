package postgres

import "testing"

func TestEmbeddedMigrationsAreCompleteAndSequential(t *testing.T) {
	migrations, err := EmbeddedMigrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) < 2 {
		t.Fatalf("expected at least two migrations, got %d", len(migrations))
	}
	for index, migration := range migrations {
		if migration.Version != int64(index+1) {
			t.Fatalf("migration %d has version %d", index, migration.Version)
		}
		if migration.Name == "" || migration.UpSQL == "" || migration.DownSQL == "" || len(migration.Checksum) != 64 {
			t.Fatalf("migration %d is incomplete", migration.Version)
		}
	}
}
