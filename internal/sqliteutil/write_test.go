package sqliteutil

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	_ "modernc.org/sqlite"
)

func TestWriteAtomicallyRejectsEmptyPath(t *testing.T) {
	err := WriteAtomically(" \t", func(db *sql.DB) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "required") {
		t.Fatalf("expected required-path error, got %v", err)
	}
}

func TestWriteAtomicallyReturnsMkdirAllError(t *testing.T) {
	restorePathOps(t)
	mkdirAll = func(path string, perm os.FileMode) error {
		return errors.New("mkdir failure")
	}

	err := WriteAtomically(filepath.Join(t.TempDir(), "artifact.sqlite"), func(db *sql.DB) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "mkdir failure") {
		t.Fatalf("expected mkdir failure, got %v", err)
	}
}

func TestWriteAtomicallyReturnsCreateTempError(t *testing.T) {
	restorePathOps(t)
	createTemp = func(dir, pattern string) (*os.File, error) {
		return nil, errors.New("temp failure")
	}

	err := WriteAtomically(filepath.Join(t.TempDir(), "artifact.sqlite"), func(db *sql.DB) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "temp failure") {
		t.Fatalf("expected temp-file failure, got %v", err)
	}
}

func TestWriteAtomicallyRemovesTempFileWhenCloseFails(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "artifact.sqlite")
	restorePathOps(t)
	var tempPath string
	createTemp = func(dir, pattern string) (*os.File, error) {
		file, err := os.CreateTemp(dir, pattern)
		if err != nil {
			return nil, err
		}
		tempPath = file.Name()
		if err := file.Close(); err != nil {
			return nil, err
		}
		return file, nil
	}

	err := WriteAtomically(outPath, func(db *sql.DB) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected close failure, got nil")
	}
	if _, statErr := os.Stat(tempPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp file to be removed, got %v", statErr)
	}
}

func TestWriteAtomicallyCleansUpTempFileOnOpenSQLiteFailure(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "artifact.sqlite")
	restorePathOps(t)
	var tempPath string
	openSQLite = func(path string) (*sql.DB, error) {
		tempPath = path
		return nil, errors.New("open failure")
	}

	err := WriteAtomically(outPath, func(db *sql.DB) error {
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "open failure") {
		t.Fatalf("expected open failure, got %v", err)
	}
	if _, statErr := os.Stat(tempPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected temp file cleanup, got %v", statErr)
	}
}

func TestWriteAtomicallyReturnsCloseError(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "artifact.sqlite")
	restorePathOps(t)
	driverName := registerCloseErrDriver(t)
	openSQLite = func(path string) (*sql.DB, error) {
		return sql.Open(driverName, path)
	}

	err := WriteAtomically(outPath, func(db *sql.DB) error {
		return db.Ping()
	})
	if err == nil || !strings.Contains(err.Error(), "close failure") {
		t.Fatalf("expected close failure, got %v", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no published artifact, got %v", statErr)
	}
}

func TestWriteAtomicallyCleansUpTempFileOnPopulateFailure(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "artifact.sqlite")
	err := WriteAtomically(outPath, func(db *sql.DB) error {
		_, execErr := db.Exec(`CREATE TABLE sample (value TEXT)`)
		if execErr != nil {
			t.Fatalf("CREATE TABLE returned error: %v", execErr)
		}
		return errors.New("boom")
	})
	if err == nil || !strings.Contains(err.Error(), "boom") {
		t.Fatalf("expected populate failure, got %v", err)
	}
	if _, statErr := os.Stat(outPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no published artifact, got %v", statErr)
	}
	entries, readErr := os.ReadDir(filepath.Dir(outPath))
	if readErr != nil {
		t.Fatalf("ReadDir returned error: %v", readErr)
	}
	for _, entry := range entries {
		if strings.Contains(entry.Name(), filepath.Base(outPath)+".tmp-") {
			t.Fatalf("expected temp files to be cleaned up, found %q", entry.Name())
		}
	}
}

func TestWriteAtomicallyRestoresExistingArtifactWhenPublishFails(t *testing.T) {
	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "artifact.sqlite")
	if err := os.WriteFile(outPath, []byte("original"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	restorePathOps(t)
	renameCalls := 0
	renamePath = func(oldPath, newPath string) error {
		renameCalls++
		if renameCalls == 2 {
			return errors.New("rename failure")
		}
		return os.Rename(oldPath, newPath)
	}

	err := WriteAtomically(outPath, func(db *sql.DB) error {
		_, execErr := db.Exec(`CREATE TABLE sample (value TEXT)`)
		return execErr
	})
	if err == nil || !strings.Contains(err.Error(), "rename failure") {
		t.Fatalf("expected publish failure, got %v", err)
	}

	content, readErr := os.ReadFile(outPath)
	if readErr != nil {
		t.Fatalf("expected original artifact to be restored, got %v", readErr)
	}
	if string(content) != "original" {
		t.Fatalf("unexpected restored content: %q", string(content))
	}
}

func TestWriteAtomicallyRejectsDirectoryTarget(t *testing.T) {
	outPath := t.TempDir()
	err := WriteAtomically(outPath, func(db *sql.DB) error {
		_, execErr := db.Exec(`CREATE TABLE sample (value TEXT)`)
		return execErr
	})
	if err == nil || !strings.Contains(err.Error(), "directory") {
		t.Fatalf("expected directory-target error, got %v", err)
	}
}

func TestWriteAtomicallyPublishesSuccessfulArtifact(t *testing.T) {
	outPath := filepath.Join(t.TempDir(), "artifact.sqlite")

	if err := WriteAtomically(outPath, func(db *sql.DB) error {
		if _, execErr := db.Exec(`CREATE TABLE sample (value TEXT)`); execErr != nil {
			return execErr
		}
		_, execErr := db.Exec(`INSERT INTO sample(value) VALUES ('ok')`)
		return execErr
	}); err != nil {
		t.Fatalf("expected successful atomic write, got %v", err)
	}

	db, err := sql.Open("sqlite", outPath)
	if err != nil {
		t.Fatalf("sql.Open returned error: %v", err)
	}
	defer db.Close()

	var value string
	if err := db.QueryRow(`SELECT value FROM sample`).Scan(&value); err != nil {
		t.Fatalf("QueryRow returned error: %v", err)
	}
	if value != "ok" {
		t.Fatalf("unexpected published value: %q", value)
	}
}

func TestPublishTempFilePublishesNewArtifact(t *testing.T) {
	root := t.TempDir()
	tempPath := filepath.Join(root, "temp.sqlite")
	outPath := filepath.Join(root, "artifact.sqlite")
	if err := os.WriteFile(tempPath, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := publishTempFile(tempPath, outPath); err != nil {
		t.Fatalf("publishTempFile returned error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "fresh" {
		t.Fatalf("unexpected artifact content: %q", string(content))
	}
	if _, err := os.Stat(tempPath); !os.IsNotExist(err) {
		t.Fatalf("expected temp path to be moved away, got %v", err)
	}
}

func TestPublishTempFileReplacesExistingArtifact(t *testing.T) {
	root := t.TempDir()
	tempPath := filepath.Join(root, "temp.sqlite")
	outPath := filepath.Join(root, "artifact.sqlite")
	if err := os.WriteFile(tempPath, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(outPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	if err := publishTempFile(tempPath, outPath); err != nil {
		t.Fatalf("publishTempFile returned error: %v", err)
	}

	content, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile returned error: %v", err)
	}
	if string(content) != "fresh" {
		t.Fatalf("unexpected artifact content: %q", string(content))
	}
	if _, err := os.Stat(tempPath + ".bak"); !os.IsNotExist(err) {
		t.Fatalf("expected backup cleanup, got %v", err)
	}
}

func TestPublishTempFileReturnsStatError(t *testing.T) {
	restorePathOps(t)
	statPath = func(path string) (os.FileInfo, error) {
		return nil, errors.New("stat failure")
	}

	err := publishTempFile("temp.sqlite", "artifact.sqlite")
	if err == nil || !strings.Contains(err.Error(), "stat failure") {
		t.Fatalf("expected stat failure, got %v", err)
	}
}

func TestPublishTempFileReturnsBackupRenameError(t *testing.T) {
	root := t.TempDir()
	tempPath := filepath.Join(root, "temp.sqlite")
	outPath := filepath.Join(root, "artifact.sqlite")
	if err := os.WriteFile(tempPath, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}
	if err := os.WriteFile(outPath, []byte("stale"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	restorePathOps(t)
	renamePath = func(oldPath, newPath string) error {
		if oldPath == outPath {
			return errors.New("backup rename failure")
		}
		return os.Rename(oldPath, newPath)
	}

	err := publishTempFile(tempPath, outPath)
	if err == nil || !strings.Contains(err.Error(), "backup rename failure") {
		t.Fatalf("expected backup rename failure, got %v", err)
	}
}

func TestPublishTempFileReturnsRenameErrorWithoutExistingTarget(t *testing.T) {
	root := t.TempDir()
	tempPath := filepath.Join(root, "temp.sqlite")
	outPath := filepath.Join(root, "artifact.sqlite")
	if err := os.WriteFile(tempPath, []byte("fresh"), 0o644); err != nil {
		t.Fatalf("WriteFile returned error: %v", err)
	}

	restorePathOps(t)
	renamePath = func(oldPath, newPath string) error {
		if oldPath == tempPath && newPath == outPath {
			return errors.New("publish rename failure")
		}
		return os.Rename(oldPath, newPath)
	}

	err := publishTempFile(tempPath, outPath)
	if err == nil || !strings.Contains(err.Error(), "publish rename failure") {
		t.Fatalf("expected publish rename failure, got %v", err)
	}
	if _, statErr := os.Stat(tempPath); statErr != nil {
		t.Fatalf("expected temp file to remain for caller cleanup, got %v", statErr)
	}
}

func restorePathOps(t *testing.T) {
	t.Helper()
	previousMkdirAll := mkdirAll
	previousCreateTemp := createTemp
	previousRenamePath := renamePath
	previousRemovePath := removePath
	previousStatPath := statPath
	previousOpenSQLite := openSQLite
	t.Cleanup(func() {
		mkdirAll = previousMkdirAll
		createTemp = previousCreateTemp
		renamePath = previousRenamePath
		removePath = previousRemovePath
		statPath = previousStatPath
		openSQLite = previousOpenSQLite
	})
}

var registerCloseErrDriverOnce sync.Once

func registerCloseErrDriver(t *testing.T) string {
	t.Helper()
	const driverName = "sqliteutil-closeerr"
	registerCloseErrDriverOnce.Do(func() {
		sql.Register(driverName, closeErrDriver{})
	})
	return driverName
}

type closeErrDriver struct{}

func (closeErrDriver) Open(name string) (driver.Conn, error) {
	return closeErrConn{}, nil
}

type closeErrConn struct{}

func (closeErrConn) Prepare(query string) (driver.Stmt, error) {
	return closeErrStmt{}, nil
}

func (closeErrConn) Close() error {
	return errors.New("close failure")
}

func (closeErrConn) Begin() (driver.Tx, error) {
	return closeErrTx{}, nil
}

func (closeErrConn) Ping(ctx context.Context) error {
	return nil
}

type closeErrStmt struct{}

func (closeErrStmt) Close() error {
	return nil
}

func (closeErrStmt) NumInput() int {
	return 0
}

func (closeErrStmt) Exec(args []driver.Value) (driver.Result, error) {
	return driver.RowsAffected(0), nil
}

func (closeErrStmt) Query(args []driver.Value) (driver.Rows, error) {
	return nil, nil
}

type closeErrTx struct{}

func (closeErrTx) Commit() error {
	return nil
}

func (closeErrTx) Rollback() error {
	return nil
}
