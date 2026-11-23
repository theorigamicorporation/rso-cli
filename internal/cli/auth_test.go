package cli_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theorigamicorporation/rso-cli/internal/config"
	"github.com/theorigamicorporation/rso-cli/internal/fakerso"
	"github.com/theorigamicorporation/rsodev-go/platform"
)

func TestLoginWithDeviceFlow(t *testing.T) {
	h := newHarness(t)
	h.api.DevicePending = 1
	r := h.ok("login")

	contains(t, r.stderr, "/device?code=ABCD-1234")
	contains(t, r.stderr, "Signed in as ada@example.com.")
	if len(h.opened) != 1 {
		t.Errorf("the browser was opened %d times, want once", len(h.opened))
	}
	absent(t, r.stdout+r.stderr, fakerso.AccessToken)

	store := config.Store{Dir: h.env["RSO_CONFIG_DIR"]}
	set, err := store.Credentials()
	if err != nil || set.AccessToken != fakerso.AccessToken || set.RefreshToken != fakerso.RefreshToken {
		t.Fatalf("saved credentials: %+v, %v", set, err)
	}
	info, err := os.Stat(filepath.Join(h.env["RSO_CONFIG_DIR"], "credentials.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("credentials.json has mode %o, want 600", mode)
	}
	// Two tenants: none is chosen for the person.
	contains(t, r.stderr, "You belong to 2 tenants")
	settings, _ := store.Settings()
	if settings.Tenant != "" {
		t.Errorf("a tenant was chosen without asking: %q", settings.Tenant)
	}
}

func TestLoginWithTokenFromStdin(t *testing.T) {
	h := newHarness(t)
	r := h.runIn(fakerso.AccessToken+"\n", "login", "--with-token")
	h.wantCode(r, 0)
	absent(t, r.stdout+r.stderr, fakerso.AccessToken)

	// A token the platform refuses is not saved over a good one.
	r = h.runIn("not-a-token\n", "login", "--with-token")
	h.wantCode(r, 3)
	set, _ := config.Store{Dir: h.env["RSO_CONFIG_DIR"]}.Credentials()
	if set.AccessToken != fakerso.AccessToken {
		t.Errorf("the saved token was replaced by one that does not work")
	}
}

func TestNotSignedIn(t *testing.T) {
	h := newHarness(t)
	r := h.run("queue", "list")
	h.wantCode(r, 3)
	contains(t, r.stderr, `run "rso login"`)
}

func TestLogoutForgetsTheSignIn(t *testing.T) {
	h := signedIn(t)
	h.ok("logout")
	h.wantCode(h.run("whoami"), 3)
	// Signing out twice is fine.
	h.ok("logout")
}

func TestWhoamiAndTenants(t *testing.T) {
	h := signedIn(t)
	h.golden("whoami", h.ok("whoami").stdout)
	h.golden("tenant-list", h.ok("tenant", "list").stdout)

	h.ok("tenant", "use", "globex")
	settings, _ := config.Store{Dir: h.env["RSO_CONFIG_DIR"]}.Settings()
	if settings.Tenant != fakerso.TenantB || settings.Region != "REGION_TWO" {
		t.Errorf("after tenant use: %+v", settings)
	}
	h.ok("quota")
	call, _ := h.api.LastCall("quota")
	if call.Tenant != fakerso.TenantB || call.Region != "REGION_TWO" {
		t.Errorf("the request went to tenant %q in region %q", call.Tenant, call.Region)
	}

	h.wantCode(h.run("tenant", "use", "nobody"), 2)
}

func TestTenantFlagAndEnvironmentWinOverTheSavedTenant(t *testing.T) {
	h := signedIn(t)
	h.env["RSO_TENANT"] = fakerso.TenantB
	h.ok("quota")
	if call, _ := h.api.LastCall("quota"); call.Tenant != fakerso.TenantB || call.Region != "" {
		t.Errorf("RSO_TENANT: tenant %q, region %q (the saved region belongs to another tenant)", call.Tenant, call.Region)
	}
	h.ok("quota", "--tenant", "explicit", "--region", "REGION_NINE")
	if call, _ := h.api.LastCall("quota"); call.Tenant != "explicit" || call.Region != "REGION_NINE" {
		t.Errorf("--tenant: tenant %q, region %q", call.Tenant, call.Region)
	}
}

func TestNoTenantChosen(t *testing.T) {
	h := newHarness(t)
	h.env["RSO_ACCESS_TOKEN"] = fakerso.AccessToken
	r := h.run("queue", "list")
	h.wantCode(r, 2)
	contains(t, r.stderr, "rso tenant use")
}

func TestServiceAccountFromTheEnvironment(t *testing.T) {
	h := newHarness(t)
	h.env["RSO_SERVICE_ACCOUNT_NAME"] = fakerso.ServiceAccount
	h.env["RSO_SERVICE_ACCOUNT_SECRET"] = fakerso.ServiceSecret
	h.env["RSO_TENANT"] = fakerso.TenantA
	h.ok("queue", "list")

	h.env["RSO_SERVICE_ACCOUNT_SECRET"] = "not-the-secret-9f3"
	r := h.run("queue", "list")
	h.wantCode(r, 3)
	absent(t, r.stderr, "not-the-secret-9f3")
}

func TestExpiredSessionIsRenewedAndSaved(t *testing.T) {
	h := signedIn(t)
	store := config.Store{Dir: h.env["RSO_CONFIG_DIR"]}
	if err := store.SaveCredentials(platform.TokenSet{AccessToken: "stale", RefreshToken: fakerso.RefreshToken, Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	h.ok("queue", "list")
	set, _ := store.Credentials()
	if set.AccessToken != fakerso.RefreshedToken {
		t.Errorf("the renewed token was not saved: %q", set.AccessToken)
	}

	// A session that cannot be renewed asks for a new sign-in.
	if err := store.SaveCredentials(platform.TokenSet{AccessToken: "stale", RefreshToken: "revoked", Expiry: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	r := h.run("queue", "list")
	h.wantCode(r, 3)
	contains(t, r.stderr, "session has expired")
}

func TestSignInSettingsComeFromTheConfigurationDocument(t *testing.T) {
	h := newHarness(t)
	delete(h.env, "RSO_TOKEN_URL")
	h.api.Configuration = map[string]string{
		"token_endpoint":                h.api.TokenURL(),
		"device_authorization_endpoint": h.api.URL + "/device/",
		"client_id":                     "from-the-document",
	}
	r := h.ok("login")
	contains(t, r.stderr, "Signed in as ada@example.com.")
	ids := h.api.ClientIDs()
	if len(ids) == 0 || ids[len(ids)-1] != "from-the-document" {
		t.Errorf("token requests used client ids %v, want the one from the document", ids)
	}
}

func TestEnvironmentWinsOverTheConfigurationDocument(t *testing.T) {
	h := newHarness(t)
	h.env["RSO_CLIENT_ID"] = "from-the-environment"
	h.api.Configuration = map[string]string{
		"token_endpoint": "https://elsewhere.example.com/token/",
		"client_id":      "from-the-document",
	}
	h.ok("login")
	for _, id := range h.api.ClientIDs() {
		if id != "from-the-environment" {
			t.Errorf("a token request used client id %q", id)
		}
	}
}

func TestLoginExplainsAClientThatCannotSignPeopleIn(t *testing.T) {
	h := newHarness(t)
	h.env["RSO_CLIENT_ID"] = "console"
	h.api.ConfidentialClient = "console"
	r := h.run("login")
	if r.code == 0 {
		t.Fatal("the sign-in went through")
	}
	contains(t, r.stderr, "Client authentication failed")
	contains(t, r.stderr, "RSO_SERVICE_ACCOUNT_NAME")
	contains(t, r.stderr, "--with-token")
}
