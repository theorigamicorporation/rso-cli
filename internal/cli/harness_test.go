package cli_test

import (
	"bytes"
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theorigamicorporation/rso-cli/internal/cli"
	"github.com/theorigamicorporation/rso-cli/internal/config"
	"github.com/theorigamicorporation/rso-cli/internal/fakerso"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata")

// harness runs the CLI against a fake API with a private config directory.
type harness struct {
	t      *testing.T
	api    *fakerso.Server
	dir    string
	env    map[string]string
	opened []string
}

// result is what one run printed and how it ended.
type result struct {
	stdout string
	stderr string
	code   int
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	api := fakerso.New()
	t.Cleanup(api.Close)
	dir := t.TempDir()
	return &harness{t: t, api: api, dir: dir, env: map[string]string{
		"RSO_CONFIG_DIR": filepath.Join(dir, "config"),
		"RSO_CACHE_DIR":  filepath.Join(dir, "cache"),
		"RSO_API_URL":    api.APIURL(),
		"RSO_TOKEN_URL":  api.TokenURL(),
	}}
}

// signedIn is a harness with a saved sign-in and tenant, as after
// "rso login" and "rso tenant use".
func signedIn(t *testing.T) *harness {
	t.Helper()
	h := newHarness(t)
	store := config.Store{Dir: h.env["RSO_CONFIG_DIR"]}
	if err := store.SaveCredentials(platform.TokenSet{AccessToken: fakerso.AccessToken, RefreshToken: fakerso.RefreshToken}); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveSettings(config.Settings{Tenant: fakerso.TenantA, TenantName: "acme", Region: "REGION_ONE"}); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *harness) run(args ...string) result { return h.runIn("", args...) }

// runIn runs the CLI with stdin holding the given text.
func (h *harness) runIn(stdin string, args ...string) result {
	h.t.Helper()
	var out, errOut bytes.Buffer
	env := cli.Env{
		In: strings.NewReader(stdin), Out: &out, Err: &errOut, Version: "test",
		Getenv:      func(key string) string { return h.env[key] },
		OpenBrowser: func(url string) error { h.opened = append(h.opened, url); return nil },
	}
	code := cli.Run(context.Background(), env, args)
	return result{stdout: out.String(), stderr: errOut.String(), code: code}
}

// ok runs the CLI and fails the test unless it succeeded.
func (h *harness) ok(args ...string) result {
	h.t.Helper()
	r := h.run(args...)
	if r.code != 0 {
		h.t.Fatalf("rso %s: exit %d\nstderr: %s", strings.Join(args, " "), r.code, r.stderr)
	}
	return r
}

// golden compares text with a file in testdata; -update rewrites the file.
// The fake API's address changes per run, so it is replaced first.
func (h *harness) golden(name, got string) {
	h.t.Helper()
	got = strings.ReplaceAll(got, h.api.URL, "https://api.example.com")
	path := filepath.Join("testdata", name+".golden")
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			h.t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		h.t.Fatalf("%v (run the tests with -update to create it)", err)
	}
	if got != string(want) {
		h.t.Errorf("%s differs from %s\n--- got\n%s\n--- want\n%s", name, path, got, want)
	}
}

func (h *harness) wantCode(r result, code int) {
	h.t.Helper()
	if r.code != code {
		h.t.Fatalf("exit code %d, want %d\nstdout: %s\nstderr: %s", r.code, code, r.stdout, r.stderr)
	}
}

func contains(t *testing.T, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("missing %q in:\n%s", want, text)
	}
}

func absent(t *testing.T, text, unwanted string) {
	t.Helper()
	if strings.Contains(text, unwanted) {
		t.Errorf("%q must not appear in:\n%s", unwanted, text)
	}
}
