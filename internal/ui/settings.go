package ui

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

// DefaultStatePath is where the production panel keeps its own small state. The
// service's state directory exists through provisioning.
const DefaultStatePath = "/var/lib/curing/panel.json"

// defaultInstallerCode only opens the installer until a code has been set, and
// the first entry with it must set one. See ADR 0308.
const defaultInstallerCode = "2580"

// Settings is the panel's own state. It holds nothing about control.
type Settings struct {
	Locked   bool   `json:"locked"`
	CodeSalt string `json:"code_salt,omitempty"`
	CodeHash string `json:"code_hash,omitempty"`
}

func hashCode(salt, code string) string {
	sum := sha256.Sum256([]byte(salt + ":" + code))
	return hex.EncodeToString(sum[:])
}

// HasOwnCode is false until the published default has been replaced.
func (s *Settings) HasOwnCode() bool { return s.CodeHash != "" }

func (s *Settings) checkCode(code string) bool {
	if !s.HasOwnCode() {
		return code == defaultInstallerCode
	}
	return hashCode(s.CodeSalt, code) == s.CodeHash
}

// SetCode stores a new code, salted and hashed.
func (s *Settings) SetCode(code string) error {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return err
	}
	s.CodeSalt = hex.EncodeToString(b[:])
	s.CodeHash = hashCode(s.CodeSalt, code)
	return nil
}

// LoadSettings reads the file. A missing file is a clean first start and no
// error. A damaged file returns zero Settings and an error for the operator.
func LoadSettings(path string) (Settings, error) {
	var s Settings
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return s, err
	}
	if err := json.Unmarshal(b, &s); err != nil {
		return Settings{}, fmt.Errorf("%s is damaged: %w", filepath.Base(path), err)
	}
	return s, nil
}

// SaveSettings replaces the file so that a power cut leaves the old or the new
// version whole, never a mix.
func SaveSettings(path string, s Settings) error {
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".panel-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	if _, err := f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}

// loadBounded reads settings without letting a stuck disk hold up the panel.
func loadBounded(path string, limit time.Duration) (Settings, error) {
	type res struct {
		s   Settings
		err error
	}
	ch := make(chan res, 1)
	go func() {
		s, err := LoadSettings(path)
		ch <- res{s, err}
	}()
	select {
	case r := <-ch:
		return r.s, r.err
	case <-time.After(limit):
		return Settings{}, errors.New("settings file did not load in time")
	}
}
