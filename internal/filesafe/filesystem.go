package filesafe

import (
	iofs "io/fs"
	"os"
	"path/filepath"
)

type filesystem interface {
	Abs(path string) (string, error)
	EvalSymlinks(path string) (string, error)
	Stat(path string) (iofs.FileInfo, error)
	Lstat(path string) (iofs.FileInfo, error)
	ReadDir(path string) ([]iofs.DirEntry, error)
	ReadFile(path string) ([]byte, error)
}

type osFilesystem struct{}

func (osFilesystem) Abs(path string) (string, error) {
	return filepath.Abs(path)
}

func (osFilesystem) EvalSymlinks(path string) (string, error) {
	return filepath.EvalSymlinks(path)
}

func (osFilesystem) Stat(path string) (iofs.FileInfo, error) {
	return os.Stat(path)
}

func (osFilesystem) Lstat(path string) (iofs.FileInfo, error) {
	return os.Lstat(path)
}

func (osFilesystem) ReadDir(path string) ([]iofs.DirEntry, error) {
	return os.ReadDir(path)
}

func (osFilesystem) ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}
