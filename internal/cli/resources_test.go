package cli_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/theorigamicorporation/rso-cli/internal/fakerso"
)

func TestScheduledJobs(t *testing.T) {
	h := signedIn(t)
	h.ok("job", "create", "nightly", "--image", "registry.example.com/acme/report:1.4", "--cron", "0 2 * * *",
		"--cpu", "0.5", "--memory", "512", "--arg", "--full", "--env", "MODE=batch", "--secret-env", "DB_URL=db/url")
	call, _ := h.api.LastCall("createScheduledJob")
	in := call.Variables["input"].(map[string]any)
	if in["image"] != "registry.example.com/acme/report:1.4" || in["schedule"] != "0 2 * * *" || in["cpu"] != 0.5 || in["memoryMb"] != float64(512) {
		t.Errorf("createScheduledJob input: %v", in)
	}
	env := in["env"].([]any)
	if len(env) != 2 || env[1].(map[string]any)["secretName"] != "db" || env[1].(map[string]any)["secretKey"] != "url" {
		t.Errorf("env: %v", env)
	}
	// Defaults the platform should choose are not sent.
	for _, key := range []string{"timeoutSeconds", "retries", "concurrency"} {
		if _, sent := in[key]; sent {
			t.Errorf("%s was sent without being asked for: %v", key, in[key])
		}
	}

	// An update changes what was given and keeps the rest.
	h.ok("job", "update", "nightly", "--cron", "30 3 * * *")
	call, _ = h.api.LastCall("updateScheduledJob")
	in = call.Variables["input"].(map[string]any)
	if in["schedule"] != "30 3 * * *" || in["image"] != "registry.example.com/acme/report:1.4" || in["memoryMb"] != float64(512) || len(in["env"].([]any)) != 2 {
		t.Errorf("updateScheduledJob input lost something: %v", in)
	}

	h.golden("job-list", h.ok("job", "list").stdout)
	h.golden("job-get", h.ok("job", "get", "nightly").stdout)
	contains(t, h.ok("job", "suspend", "nightly").stdout, "Suspended:       yes")
	h.golden("job-runs", h.ok("job", "runs", "nightly").stdout)

	// Logs are the plain lines for people, the whole answer for programs.
	if out := h.ok("job", "logs", "nightly").stdout; out != "starting\ndone\n" {
		t.Errorf("logs: %q", out)
	}
	contains(t, h.ok("job", "logs", "nightly", "-o", "json").stdout, `"pod": "nightly-manual-abc123-x"`)

	h.wantCode(h.run("job", "create", "bad", "--image", "x"), 2)
	h.wantCode(h.run("job", "create", "bad", "--image", "x", "--cron", "* * * * *", "--secret-env", "NOPE"), 2)
	h.ok("job", "delete", "nightly", "--yes")
	h.wantCode(h.run("job", "get", "nightly"), 4)
}

func TestBuckets(t *testing.T) {
	h := signedIn(t)
	h.golden("bucket-list", h.ok("bucket", "list").stdout)
	h.golden("bucket-notifications", h.ok("bucket", "notification", "list").stdout)

	h.ok("bucket", "notification", "create", "uploads", "--event", "created,removed", "--prefix", "incoming/", "--suffix", ".csv", "--queue", "imports")
	call, _ := h.api.LastCall("createBucketNotification")
	in := call.Variables["input"].(map[string]any)
	events := in["events"].([]any)
	if len(events) != 2 || events[0] != "CREATED" || events[1] != "REMOVED" || in["keyPrefix"] != "incoming/" || in["queue"] != "imports" {
		t.Errorf("createBucketNotification input: %v", in)
	}
	h.wantCode(h.run("bucket", "notification", "create", "uploads", "--event", "renamed", "--queue", "imports"), 2)
	h.wantCode(h.run("bucket", "notification", "create", "uploads"), 2)

	h.ok("bucket", "notification", "delete", "uploads", "n-1", "--yes")
	call, _ = h.api.LastCall("deleteBucketNotification")
	if call.Variables["bucket"] != "uploads" || call.Variables["id"] != "n-1" {
		t.Errorf("deleteBucketNotification: %v", call.Variables)
	}

	contains(t, h.ok("bucket", "key", "create", "backup").stdout, "test-secret-access-key")
	absent(t, h.ok("bucket", "key", "list", "-o", "yaml").stdout, "secret")
}

func TestKubernetes(t *testing.T) {
	h := signedIn(t)
	h.golden("kubernetes-list", h.ok("kubernetes", "list").stdout)
	h.golden("kubernetes-get", h.ok("k8s", "get", "production").stdout)

	if out := h.ok("kubernetes", "kubeconfig", "production").stdout; out != "apiVersion: v1\nkind: Config\n" {
		t.Errorf("kubeconfig: %q", out)
	}
	if call, _ := h.api.LastCall("tenantClusterKubeconfig"); call.Variables["kind"] != "EXEC" {
		t.Errorf("default kubeconfig kind: %v", call.Variables["kind"])
	}
	h.ok("kubernetes", "kubeconfig", "production", "--session")
	if call, _ := h.api.LastCall("tenantClusterKubeconfig"); call.Variables["kind"] != "SESSION" {
		t.Errorf("--session kubeconfig kind: %v", call.Variables["kind"])
	}
}

// A kubeconfig written by the platform runs exactly this command line; it
// has to keep working, and print nothing but the credential.
func TestKubernetesTokenAsKubectlRunsIt(t *testing.T) {
	h := newHarness(t)
	h.env["RSO_SERVICE_ACCOUNT_NAME"] = fakerso.ServiceAccount
	h.env["RSO_SERVICE_ACCOUNT_SECRET"] = fakerso.ServiceSecret
	api := h.env["RSO_API_URL"]
	delete(h.env, "RSO_API_URL")

	args := []string{"kubernetes", "token", "--cluster=production", "--tenant=" + fakerso.TenantA, "--api=" + api}
	r := h.ok(args...)
	if !strings.HasPrefix(r.stdout, `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential"`) || r.stderr != "" {
		t.Fatalf("stdout %q, stderr %q", r.stdout, r.stderr)
	}
	call, _ := h.api.LastCall("tenantClusterToken")
	if call.Variables["name"] != "production" || call.Variables["ttl"] != float64(900) || call.Tenant != fakerso.TenantA {
		t.Errorf("tenantClusterToken: %v for tenant %q", call.Variables, call.Tenant)
	}

	// The second run comes from the private cache, not from the platform.
	before := len(h.api.Calls())
	if again := h.ok(args...); again.stdout != r.stdout {
		t.Errorf("the cached credential differs")
	}
	if len(h.api.Calls()) != before {
		t.Errorf("the platform was asked again despite a good cached credential")
	}
	files, _ := filepath.Glob(filepath.Join(h.env["RSO_CACHE_DIR"], "*.json"))
	if len(files) != 1 {
		t.Fatalf("cache files: %v", files)
	}
	if info, _ := os.Stat(files[0]); info.Mode().Perm() != 0o600 {
		t.Errorf("the cached credential has mode %o, want 600", info.Mode().Perm())
	}

	h.env["RSO_NO_CACHE"] = "1"
	h.ok(args...)
	if len(h.api.Calls()) == before {
		t.Errorf("RSO_NO_CACHE did not skip the cache")
	}
	h.wantCode(h.run("kubernetes", "token", "--tenant="+fakerso.TenantA, "--api="+api), 2)
}

// Nobody names a cluster: the platform knows which one serves the tenant.
func TestApps(t *testing.T) {
	h := signedIn(t)
	h.golden("app-list", h.ok("app", "list").stdout)
	call, _ := h.api.LastCall("apps")
	if _, sent := call.Variables["cluster"]; sent {
		t.Errorf("apps was asked with a cluster: %v", call.Variables)
	}
	h.golden("app-get", h.ok("app", "get", "web").stdout)

	r := h.ok("app", "logs", "web", "--tail", "50")
	if r.stdout != "listening on :8080\n" {
		t.Errorf("logs: %q", r.stdout)
	}
	contains(t, r.stderr, "Older lines were left out")

	// A new app needs an image; what is given is what is sent.
	h.wantCode(h.run("app", "deploy", "api", "--port", "8080"), 2)
	h.ok("app", "deploy", "api", "--image", "registry.example.com/acme/api:1.0", "--port", "9000",
		"--env", "MODE=fast", "--secret-env", "DB_URL=db:url", "--memory", "512Mi", "--pull-secret", "images")
	call, _ = h.api.LastCall("createApp")
	in := call.Variables["input"].(map[string]any)
	spec := in["template"].(map[string]any)["spec"].(map[string]any)
	container := spec["containers"].([]any)[0].(map[string]any)
	env := container["env"].([]any)
	if in["name"] != "api" || container["image"] != "registry.example.com/acme/api:1.0" ||
		container["ports"].([]any)[0].(map[string]any)["containerPort"] != float64(9000) || len(env) != 2 ||
		env[1].(map[string]any)["valueFrom"].(map[string]any)["secretKeyRef"].(map[string]any)["key"] != "url" ||
		container["resources"].(map[string]any)["limits"].(map[string]any)["memory"] != "512Mi" ||
		spec["imagePullSecrets"].([]any)[0].(map[string]any)["name"] != "images" {
		t.Errorf("createApp input: %v", in)
	}

	// An existing app gets a new revision and keeps what was not given.
	h.ok("app", "deploy", "web", "--image", "registry.example.com/acme/web:1.1", "--env", "LOG_LEVEL=debug", "--env", "EXTRA=1")
	if _, created := h.api.LastCall("updateApp"); !created {
		t.Fatal("an existing app was not updated")
	}
	call, _ = h.api.LastCall("updateApp")
	container = call.Variables["input"].(map[string]any)["template"].(map[string]any)["spec"].(map[string]any)["containers"].([]any)[0].(map[string]any)
	env = container["env"].([]any)
	if container["image"] != "registry.example.com/acme/web:1.1" || container["ports"].([]any)[0].(map[string]any)["containerPort"] != float64(8080) ||
		len(env) != 2 || env[0].(map[string]any)["value"] != "debug" {
		t.Errorf("updateApp container: %v", container)
	}
	h.wantCode(h.run("app", "deploy", "web", "--secret-env", "NOPE"), 2)

	h.wantCode(h.run("app", "delete", "web"), 2)
	h.ok("app", "delete", "web", "--yes")
	h.wantCode(h.run("app", "get", "web"), 4)
}

func TestQuota(t *testing.T) {
	h := signedIn(t)
	h.golden("quota", h.ok("quota").stdout)
	h.golden("quota-yaml", h.ok("quota", "-o", "yaml").stdout)
}

func TestAIChat(t *testing.T) {
	h := signedIn(t)
	h.golden("ai-models", h.ok("ai", "models").stdout)

	r := h.ok("ai", "chat", "--model", "small-model:latest", "Say hello")
	if r.stdout != "Hello, world\n" {
		t.Errorf("answer: %q", r.stdout)
	}
	contains(t, r.stderr, "Tokens: 4 in, 3 out.")

	r = h.runIn("Say hello", "ai", "chat", "--model", "small-model:latest", "-o", "json")
	h.wantCode(r, 0)
	contains(t, r.stdout, `"answer": "Hello, world"`)
	contains(t, r.stdout, `"prompt_tokens": 4`)

	r = h.run("ai", "chat", "--model", "over-quota", "hi")
	h.wantCode(r, 7)
	contains(t, r.stderr, "token quota")
	h.wantCode(h.run("ai", "chat", "hi"), 2)
}

func TestVersionAndCompletion(t *testing.T) {
	h := newHarness(t)
	contains(t, h.ok("version").stdout, "rso:")
	contains(t, h.ok("version", "-o", "json").stdout, `"version": "test"`)
	contains(t, h.ok("completion", "bash").stdout, "__start_rso")
}
