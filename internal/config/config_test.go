package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/theorigamicorporation/rso-cli/internal/config"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

func TestNothingSavedIsNotAnError(t *testing.T) {
	store := config.Store{Dir: filepath.Join(t.TempDir(), "missing")}
	if settings, err := store.Settings(); err != nil || settings != (config.Settings{}) {
		t.Errorf("%+v, %v", settings, err)
	}
	if set, err := store.Credentials(); err != nil || set.AccessToken != "" {
		t.Errorf("%+v, %v", set, err)
	}
	if err := store.DeleteCredentials(); err != nil {
		t.Error(err)
	}
}

func TestFilesArePrivate(t *testing.T) {
	store := config.Store{Dir: filepath.Join(t.TempDir(), "rso")}
	if err := store.SaveCredentials(platform.TokenSet{AccessToken: "a", RefreshToken: "r"}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(config.Settings{Tenant: "t", Region: "R"}); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"credentials.json", "config.yaml"} {
		info, err := os.Stat(filepath.Join(store.Dir, name))
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s has mode %o, want 600", name, mode)
		}
	}
	if info, _ := os.Stat(store.Dir); info.Mode().Perm() != 0o700 {
		t.Errorf("the directory has mode %o, want 700", info.Mode().Perm())
	}
	set, err := store.Credentials()
	if err != nil || set.AccessToken != "a" || set.RefreshToken != "r" {
		t.Errorf("%+v, %v", set, err)
	}
	settings, err := store.Settings()
	if err != nil || settings.Tenant != "t" || settings.Region != "R" {
		t.Errorf("%+v, %v", settings, err)
	}
	// No temporary files are left behind.
	entries, _ := os.ReadDir(store.Dir)
	if len(entries) != 2 {
		t.Errorf("%d files in the directory, want 2", len(entries))
	}
}

func TestDir(t *testing.T) {
	dir, err := config.Dir(func(key string) string {
		if key == "RSO_CONFIG_DIR" {
			return "/somewhere/else"
		}
		return ""
	})
	if err != nil || dir != "/somewhere/else" {
		t.Errorf("%q, %v", dir, err)
	}
}
