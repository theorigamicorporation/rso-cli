package cli_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInstances(t *testing.T) {
	h := signedIn(t)
	h.golden("instance-list", h.ok("instance", "list").stdout)
	h.golden("instance-get", h.ok("instance", "get", "build-1").stdout)
	contains(t, h.ok("instance", "templates").stdout, "ubuntu-26.04")

	// By name or by id, the action goes to the instance's id.
	h.ok("instance", "stop", "build-1")
	if call, _ := h.api.LastCall("stopInstance"); call.Variables["id"] != float64(4101) {
		t.Errorf("stopInstance: %v", call.Variables)
	}
	h.ok("instance", "start", "4101")
	h.ok("instance", "reboot", "build-1")
	h.wantCode(h.run("instance", "stop", "missing"), 4)

	h.wantCode(h.run("instance", "create", "build-1"), 2)
	h.ok("instance", "create", "build-1", "--template", "901", "--cpus", "4", "--memory", "8192", "--ssh-key", "key-1")
	call, _ := h.api.LastCall("createInstance")
	in := call.Variables["input"].(map[string]any)
	if in["name"] != "build-1" || in["templateVmId"] != "901" || in["cpus"] != float64(4) || in["memory"] != float64(8192) || in["sshKeyIds"].([]any)[0] != "key-1" {
		t.Errorf("createInstance input: %v", in)
	}
	contains(t, h.ok("instance", "create", "x", "--template", "901", "--no-wait").stdout, "task-1")

	h.wantCode(h.run("instance", "delete", "build-1"), 2)
	h.ok("instance", "delete", "build-1", "--yes")
	if call, _ := h.api.LastCall("deleteInstance"); call.Variables["id"] != float64(4101) {
		t.Errorf("deleteInstance: %v", call.Variables)
	}
}

func TestPostgres(t *testing.T) {
	h := signedIn(t)
	h.golden("postgres-list", h.ok("postgres", "list").stdout)
	h.golden("postgres-get", h.ok("pg", "get", "orders").stdout)

	h.ok("postgres", "create", "billing", "--members", "3", "--storage", "50Gi", "--memory", "2Gi")
	call, _ := h.api.LastCall("createPostgresCluster")
	in := call.Variables["input"].(map[string]any)
	spec := in["spec"].(map[string]any)
	if in["name"] != "billing" || spec["instances"] != float64(3) || spec["storage"].(map[string]any)["size"] != "50Gi" ||
		spec["resources"].(map[string]any)["limits"].(map[string]any)["memory"] != "2Gi" {
		t.Errorf("createPostgresCluster input: %v", in)
	}

	h.ok("postgres", "delete", "orders", "--yes", "--keep-backups")
	if call, _ := h.api.LastCall("deletePostgresCluster"); call.Variables["name"] != "orders" || call.Variables["keep"] != true {
		t.Errorf("deletePostgresCluster: %v", call.Variables)
	}

	contains(t, h.ok("postgres", "database", "list", "orders").stdout, "shop")
	h.ok("postgres", "database", "create", "orders", "reports", "--owner", "analyst")
	call, _ = h.api.LastCall("createPostgresDatabase")
	if db := call.Variables["input"].(map[string]any); db["clusterName"] != "orders" || db["name"] != "reports" || db["owner"] != "analyst" {
		t.Errorf("createPostgresDatabase input: %v", db)
	}
	h.ok("postgres", "database", "delete", "orders", "reports", "--yes")

	h.golden("postgres-backups", h.ok("postgres", "backup", "list", "orders").stdout)
	h.ok("postgres", "backup", "enable", "orders", "--retention", "14d")
	if call, _ := h.api.LastCall("enablePostgresBackups"); call.Variables["retention"] != "14d" {
		t.Errorf("enablePostgresBackups: %v", call.Variables)
	}
	h.wantCode(h.run("postgres", "backup", "schedule", "orders"), 2)
	h.ok("postgres", "backup", "schedule", "orders", "--cron", "0 4 * * *")
	h.ok("postgres", "backup", "unschedule", "orders")
	contains(t, h.ok("postgres", "backup", "now", "orders").stdout, "orders-manual")

	h.wantCode(h.run("postgres", "backup", "restore", "orders"), 2)
	h.wantCode(h.run("postgres", "backup", "restore", "orders", "--to", "copy", "--backup", "b", "--at", "2026-01-02T03:00:00Z"), 2)
	h.ok("postgres", "backup", "restore", "orders", "--to", "orders-copy", "--at", "2026-01-02T03:00:00Z")
	call, _ = h.api.LastCall("restorePostgresCluster")
	if in := call.Variables["input"].(map[string]any); in["name"] != "orders-copy" || in["sourceCluster"] != "orders" || in["targetTime"] != "2026-01-02T03:00:00Z" {
		t.Errorf("restorePostgresCluster input: %v", in)
	}
}

func TestRegistriesVolumesAndKeys(t *testing.T) {
	h := signedIn(t)
	h.golden("registry-list", h.ok("registry", "list").stdout)
	h.ok("registry", "create", "builds")
	r := h.ok("registry", "credentials", "images", "--push")
	contains(t, r.stdout, "test-registry-password")
	if call, _ := h.api.LastCall("issueDockerRegistryCredentials"); call.Variables["input"].(map[string]any)["name"] != "images" {
		t.Errorf("issueDockerRegistryCredentials: %v", call.Variables)
	}
	h.ok("registry", "delete", "images", "--yes")

	h.golden("volume-list", h.ok("volume", "list").stdout)
	h.wantCode(h.run("volume", "create", "data"), 2)
	h.ok("volume", "create", "data", "--size", "20")
	if call, _ := h.api.LastCall("createVolume"); call.Variables["input"].(map[string]any)["name"] != "data" {
		t.Errorf("createVolume: %v", call.Variables)
	}
	h.ok("volume", "delete", "data", "--yes")

	h.golden("ssh-key-list", h.ok("ssh-key", "list").stdout)
	h.runIn("ssh-ed25519 AAAAC3Nza laptop\n", "ssh-key", "add", "laptop")
	call, _ := h.api.LastCall("addSshKey")
	if in := call.Variables["input"].(map[string]any); in["name"] != "laptop" || in["publicKey"] != "ssh-ed25519 AAAAC3Nza laptop" {
		t.Errorf("addSshKey input: %v", in)
	}
	file := filepath.Join(t.TempDir(), "id.pub")
	if err := os.WriteFile(file, []byte("ssh-ed25519 AAAAC3Nza file\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	h.ok("ssh-key", "add", "desk", "--file", file)
	h.wantCode(h.run("ssh-key", "add", "empty"), 2)
	h.ok("ssh-key", "delete", "key-1", "--yes")
}

func TestIngress(t *testing.T) {
	h := signedIn(t)
	h.golden("service-list", h.ok("service", "list").stdout)
	h.ok("service", "create", "api", "--target", "api.hosts.example.com", "--port", "80:8080", "--port", "443")
	call, _ := h.api.LastCall("createExternalService")
	in := call.Variables["input"].(map[string]any)
	ports := in["ports"].([]any)
	if in["externalName"] != "api.hosts.example.com" || len(ports) != 2 || ports[0].(map[string]any)["targetPort"] != float64(8080) || ports[1].(map[string]any)["targetPort"] != float64(443) {
		t.Errorf("createExternalService input: %v", in)
	}
	h.wantCode(h.run("service", "create", "api", "--target", "x", "--port", "http"), 2)
	h.ok("service", "delete", "api", "--yes")

	h.golden("ingress-list", h.ok("ingress", "list").stdout)
	h.ok("ingress", "create", "api", "--host", "example.com", "--path-prefix", "/api", "--service", "api:8080", "--middleware", "strip-api")
	call, _ = h.api.LastCall("createIngressRoute")
	rule := call.Variables["input"].(map[string]any)["routes"].([]any)[0].(map[string]any)
	if rule["match"] != "Host(`example.com`) && PathPrefix(`/api`)" || rule["middlewares"].([]any)[0].(map[string]any)["name"] != "strip-api" ||
		rule["services"].([]any)[0].(map[string]any)["port"] != float64(8080) {
		t.Errorf("createIngressRoute rule: %v", rule)
	}
	h.wantCode(h.run("ingress", "create", "api", "--service", "api:80"), 2)
	h.wantCode(h.run("ingress", "create", "api", "--host", "a", "--match", "b", "--service", "api:80"), 2)
	h.wantCode(h.run("ingress", "create", "api", "--host", "a", "--service", "api"), 2)
	h.ok("ingress", "delete", "api", "--yes")

	h.golden("middleware-list", h.ok("middleware", "list").stdout)
	h.ok("middleware", "create", "strip", "--strip-prefix", "/a,/b")
	call, _ = h.api.LastCall("createMiddleware")
	if in := call.Variables["input"].(map[string]any); in["type"] != "stripPrefix" || len(in["stripPrefix"].(map[string]any)["prefixes"].([]any)) != 2 {
		t.Errorf("createMiddleware input: %v", in)
	}
	// The password comes from standard input and is never echoed.
	r := h.runIn("s3cret\n", "middleware", "create", "staff", "--basic-auth-user", "ada")
	h.wantCode(r, 0)
	absent(t, r.stdout+r.stderr, "s3cret")
	call, _ = h.api.LastCall("createMiddleware")
	if auth := call.Variables["input"].(map[string]any)["basicAuth"].(map[string]any); auth["username"] != "ada" || auth["password"] != "s3cret" {
		t.Errorf("basic auth: %v", auth)
	}
	h.wantCode(h.run("middleware", "create", "staff", "--basic-auth-user", "ada"), 2)
	h.wantCode(h.run("middleware", "create", "both", "--strip-prefix", "/a", "--redirect-scheme", "https"), 2)
	h.wantCode(h.run("middleware", "create", "none"), 2)
	h.ok("middleware", "delete", "strip", "--yes")
}

func TestDNS(t *testing.T) {
	h := signedIn(t)
	contains(t, h.ok("dns", "zones").stdout, "example.com")
	h.golden("dns-records", h.ok("dns", "record", "list", "--zone", "example.com").stdout)
	if call, _ := h.api.LastCall("dnsRecords"); call.Variables["zone"] != "example.com" {
		t.Errorf("dnsRecords: %v", call.Variables)
	}
	contains(t, h.ok("dns", "record", "get", "33333333-3333-4333-8333-333333333333").stdout, "shop.example.com")

	h.ok("dns", "record", "create", "example.com", "www.example.com", "shop.example.com", "--type", "cname")
	call, _ := h.api.LastCall("createDnsRecord")
	if in := call.Variables["input"].(map[string]any); in["zone"] != "example.com" || in["name"] != "www.example.com" || in["type"] != "CNAME" || in["content"] != "shop.example.com" {
		t.Errorf("createDnsRecord input: %v", in)
	}
	h.wantCode(h.run("dns", "record", "create", "example.com", "mail.example.com", "x", "--type", "MX"), 2)

	contains(t, h.ok("dns", "record", "update", "33333333-3333-4333-8333-333333333333", "203.0.113.11").stdout, "203.0.113.11")
	h.wantCode(h.run("dns", "record", "delete", "33333333-3333-4333-8333-333333333333"), 2)
	h.ok("dns", "record", "delete", "33333333-3333-4333-8333-333333333333", "--yes")

	h.api.Fail("createDnsRecord", "CONFLICT", "The name is held by another tenant")
	h.wantCode(h.run("dns", "record", "create", "example.com", "taken.example.com", "203.0.113.12"), 6)
}

func TestAccess(t *testing.T) {
	h := signedIn(t)
	h.golden("service-account-list", h.ok("service-account", "list").stdout)
	r := h.ok("service-account", "create", "releaser", "--expires-in-days", "30")
	contains(t, r.stdout, "test-new-service-secret")
	h.wantCode(h.run("service-account", "create", "releaser", "--expires-in-days", "0"), 2)

	// By name: the id is looked up.
	contains(t, h.ok("sa", "rotate", "ci-deployer").stdout, "test-rotated-service-secret")
	if call, _ := h.api.LastCall("rotateServiceAccountToken"); call.Variables["id"] != "sa-1" {
		t.Errorf("rotateServiceAccountToken: %v", call.Variables)
	}
	h.ok("service-account", "disable", "ci-deployer")
	if call, _ := h.api.LastCall("setServiceAccountActive"); call.Variables["input"].(map[string]any)["id"] != "sa-1" {
		t.Errorf("setServiceAccountActive: %v", call.Variables)
	}
	h.wantCode(h.run("service-account", "rotate", "missing"), 4)
	h.ok("service-account", "delete", "ci-deployer", "--yes")

	h.golden("access-list", h.ok("access", "list").stdout)
	h.golden("access-get", h.ok("access", "get", "ci-deployer").stdout)
	h.wantCode(h.run("access", "get", "nobody"), 4)
	contains(t, h.ok("access", "roles").stdout, "EDITOR")
	contains(t, h.ok("access", "resource-types").stdout, "bucket")

	h.ok("access", "grant", "ci-deployer", "apps.read", "apps.write")
	if call, _ := h.api.LastCall("grantPermissions"); call.Variables["username"] != "ci-deployer" || len(call.Variables["permissions"].([]any)) != 2 {
		t.Errorf("grantPermissions: %v", call.Variables)
	}
	h.ok("access", "revoke", "ci-deployer", "apps.write")

	// A scoped grant is added to the ones the account already has.
	h.ok("access", "scope", "ci-deployer", "--type", "app", "--label", "team=shop", "--role", "EDITOR")
	call, _ := h.api.LastCall("setPrincipalAccess")
	scoped := call.Variables["input"].(map[string]any)["scoped"].([]any)
	if len(scoped) != 2 || scoped[0].(map[string]any)["resourceType"] != "bucket" {
		t.Fatalf("scoped grants: %v", scoped)
	}
	added := scoped[1].(map[string]any)
	label := added["selector"].(map[string]any)["labels"].([]any)[0].(map[string]any)
	if added["resourceType"] != "app" || label["key"] != "team" || label["value"] != "shop" || added["roles"].([]any)[0] != "EDITOR" {
		t.Errorf("added grant: %v", added)
	}
	if _, sent := call.Variables["input"].(map[string]any)["roles"]; sent {
		t.Errorf("tenant-wide roles were sent with a scoped grant")
	}
	h.ok("access", "scope", "ci-deployer", "--type", "app", "--name", "web", "--permission", "apps.read", "--replace")
	call, _ = h.api.LastCall("setPrincipalAccess")
	if scoped := call.Variables["input"].(map[string]any)["scoped"].([]any); len(scoped) != 1 {
		t.Errorf("--replace kept earlier grants: %v", scoped)
	}
	h.wantCode(h.run("access", "scope", "ci-deployer", "--type", "app", "--role", "EDITOR"), 2)
	h.wantCode(h.run("access", "scope", "ci-deployer", "--name", "web", "--role", "EDITOR"), 2)
}
