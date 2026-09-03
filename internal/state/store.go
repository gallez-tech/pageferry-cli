package state

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type Config struct {
	APIURL string `json:"apiUrl,omitempty"`
}

type Credentials struct {
	APIKey    string    `json:"apiKey,omitempty"`
	UpdatedAt time.Time `json:"updatedAt,omitempty"`
}

type Draft struct {
	DraftID             string    `json:"draftId"`
	PublicURL           string    `json:"publicUrl"`
	RawURL              string    `json:"rawUrl"`
	LatestVersionNumber int       `json:"latestVersionNumber"`
	HostingMode         string    `json:"hostingMode,omitempty"`
	ExpiresAt           string    `json:"expiresAt,omitempty"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

type Store struct {
	Dir string
}

func New(dir string) *Store { return &Store{Dir: dir} }

func Default() (*Store, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return New(filepath.Join(home, ".pageferry")), nil
}

func (s *Store) LoadConfig() Config {
	var value Config
	s.load("config.json", &value)
	return value
}

func (s *Store) SaveConfig(value Config) error { return s.save("config.json", value) }

func (s *Store) LoadCredentials() Credentials {
	var value Credentials
	s.load("credentials.json", &value)
	return value
}

func (s *Store) SaveCredentials(value Credentials) error {
	return s.save("credentials.json", value)
}

func (s *Store) LoadDrafts() map[string]Draft {
	value := make(map[string]Draft)
	s.load("drafts.json", &value)
	if value == nil {
		return make(map[string]Draft)
	}
	return value
}

func (s *Store) SaveDrafts(value map[string]Draft) error { return s.save("drafts.json", value) }

func (s *Store) load(name string, target any) {
	data, err := os.ReadFile(filepath.Join(s.Dir, name))
	if err != nil || json.Unmarshal(data, target) != nil {
		return
	}
}

func (s *Store) save(name string, value any) error {
	if err := os.MkdirAll(s.Dir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(s.Dir, 0o700); err != nil && !errors.Is(err, os.ErrPermission) {
		return err
	}
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	path := filepath.Join(s.Dir, name)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return err
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	return file.Close()
}
