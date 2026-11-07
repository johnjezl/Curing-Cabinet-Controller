package drivers

import (
	"os"
	"path/filepath"
)

// fileSystem is the slice of sysfs the drivers use.
type fileSystem interface {
	ReadFile(path string) ([]byte, error)
	WriteFile(path string, data []byte) error
	Glob(pattern string) ([]string, error)
	EvalSymlinks(path string) (string, error)
}

type osFS struct{}

func (osFS) ReadFile(p string) ([]byte, error) { return os.ReadFile(p) }
func (osFS) Glob(p string) ([]string, error)   { return filepath.Glob(p) }
func (osFS) EvalSymlinks(p string) (string, error) {
	return filepath.EvalSymlinks(p)
}
func (osFS) WriteFile(p string, data []byte) error {
	f, err := os.OpenFile(p, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
