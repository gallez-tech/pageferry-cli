package state

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStorePermissionsAndInvalidJSON(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "state")
	store := New(directory)
	if err := store.SaveCredentials(Credentials{APIKey: "secret"}); err != nil {
		t.Fatal(err)
	}
	dirInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	fileInfo, err := os.Stat(filepath.Join(directory, "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got := dirInfo.Mode().Perm(); got != 0o700 {
		t.Errorf("directory mode = %o", got)
	}
	if got := fileInfo.Mode().Perm(); got != 0o600 {
		t.Errorf("credentials mode = %o", got)
	}
	if err := os.WriteFile(filepath.Join(directory, "drafts.json"), []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if drafts := store.LoadDrafts(); len(drafts) != 0 {
		t.Errorf("invalid JSON returned drafts: %v", drafts)
	}
}
