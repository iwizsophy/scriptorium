package sqliteutil

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

var (
	mkdirAll   = os.MkdirAll
	createTemp = os.CreateTemp
	renamePath = os.Rename
	removePath = os.Remove
	statPath   = os.Stat
	openSQLite = func(path string) (*sql.DB, error) {
		return sql.Open("sqlite", path)
	}
)

func WriteAtomically(outPath string, populate func(*sql.DB) error) error {
	if strings.TrimSpace(outPath) == "" {
		return fmt.Errorf("output path is required")
	}

	if err := mkdirAll(filepath.Dir(outPath), 0o755); err != nil {
		return err
	}

	tempFile, err := createTemp(filepath.Dir(outPath), filepath.Base(outPath)+".tmp-*.sqlite")
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	if err := tempFile.Close(); err != nil {
		_ = removePath(tempPath)
		return err
	}

	cleanupTemp := true
	defer func() {
		if cleanupTemp {
			_ = removePath(tempPath)
		}
	}()

	db, err := openSQLite(tempPath)
	if err != nil {
		return err
	}

	populateErr := populate(db)
	closeErr := db.Close()
	if populateErr != nil {
		return populateErr
	}
	if closeErr != nil {
		return closeErr
	}

	if err := publishTempFile(tempPath, outPath); err != nil {
		return err
	}
	cleanupTemp = false
	return nil
}

func publishTempFile(tempPath, outPath string) error {
	info, err := statPath(outPath)
	targetExists := false
	backupPath := ""
	switch {
	case err == nil:
		if info.IsDir() {
			return fmt.Errorf("output path is a directory: %s", outPath)
		}
		targetExists = true
		backupPath = tempPath + ".bak"
		if err := renamePath(outPath, backupPath); err != nil {
			return err
		}
	case os.IsNotExist(err):
	default:
		return err
	}

	if err := renamePath(tempPath, outPath); err != nil {
		if targetExists {
			_ = renamePath(backupPath, outPath)
		}
		return err
	}

	if targetExists {
		_ = removePath(backupPath)
	}
	return nil
}
