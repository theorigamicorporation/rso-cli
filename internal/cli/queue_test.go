package cli_test

import (
	"encoding/json"
	"testing"

	"github.com/theorigamicorporation/rsodev-go/platform"
)

func TestQueuesEndToEnd(t *testing.T) {
	h := signedIn(t)
	h.ok("queue", "create", "orders")
	h.ok("queue", "create", "audit")
	h.ok("topic", "create", "events")
	h.ok("topic", "subscribe", "events", "audit")
	h.ok("topic", "subscribe", "events", "orders", "--filter", "order.*")

	h.ok("queue", "publish", "orders", "direct", "--attribute", "source=test")
	h.ok("topic", "publish", "events", `{"id":7}`, "--routing-key", "order.created", "--json")
	h.ok("topic", "publish", "events", "hello", "--routing-key", "user.created")
	// A message from standard input.
	h.wantCode(h.runIn("from stdin", "queue", "publish", "audit"), 0)

	h.golden("queue-list", h.ok("queue", "list").stdout)
	h.golden("topic-list", h.ok("topic", "list").stdout)
	h.golden("subscriptions", h.ok("topic", "subscriptions", "--topic", "events").stdout)

	// orders got the direct message and the order event, not the user event.
	var messages []platform.ReceivedMessage
	r := h.ok("queue", "receive", "orders", "--max", "10", "-o", "json")
	if err := json.Unmarshal([]byte(r.stdout), &messages); err != nil {
		t.Fatalf("%v in %s", err, r.stdout)
	}
	if len(messages) != 2 || messages[0].Body != "direct" || messages[1].Body != `{"id":7}` || messages[1].RoutingKey != "order.created" {
		t.Errorf("orders received %+v", messages)
	}
	if messages[0].Attributes["source"] != "test" {
		t.Errorf("attributes were lost: %+v", messages[0].Attributes)
	}
	// Receiving removed them.
	contains(t, h.ok("queue", "receive", "orders").stdout, "The queue is empty.")
	// An empty receive is still a JSON list for programs.
	if out := h.ok("queue", "receive", "orders", "-o", "json").stdout; out != "[]\n" {
		t.Errorf("empty receive as JSON: %q", out)
	}
	h.golden("queue-receive", h.ok("queue", "receive", "audit", "--max", "10").stdout)
}

func TestPublishThatNobodyReceivesSaysSo(t *testing.T) {
	h := signedIn(t)
	h.ok("topic", "create", "events")
	r := h.ok("topic", "publish", "events", "lost")
	contains(t, r.stderr, "no subscription matched")
	contains(t, r.stdout, "Routed:")
}

func TestPublishRejectsBrokenJSON(t *testing.T) {
	h := signedIn(t)
	h.ok("queue", "create", "orders")
	h.wantCode(h.run("queue", "publish", "orders", "{not json", "--json"), 2)
}

func TestDeletingAsksFirst(t *testing.T) {
	h := signedIn(t)
	h.ok("queue", "create", "orders")

	// No terminal to ask on and no --yes: refuse, and delete nothing.
	r := h.run("queue", "delete", "orders")
	h.wantCode(r, 2)
	contains(t, r.stderr, "--yes")
	if _, called := h.api.LastCall("deleteQueue"); called {
		t.Fatal("the queue was deleted without confirmation")
	}

	h.ok("queue", "delete", "orders", "--yes")
	h.wantCode(h.run("queue", "get", "orders"), 4)
	h.wantCode(h.run("queue", "delete", "orders", "-y"), 4)
}

func TestErrorsMapToExitCodes(t *testing.T) {
	h := signedIn(t)
	h.ok("queue", "create", "orders")
	h.wantCode(h.run("queue", "create", "orders"), 6)

	h.api.Fail("createTopic", "QUOTA_EXCEEDED", "Quota exceeded for topics: using 20, requested 1 more, limit 20")
	r := h.run("topic", "create", "one-too-many")
	h.wantCode(r, 7)
	contains(t, r.stderr, "Quota exceeded for topics")

	h.api.Fail("queues", "AUTHORIZATION_ERROR", `Permission "queues.read" is required`)
	h.wantCode(h.run("queue", "list"), 5)

	h.api.Fail("queuesInfo", "NOT_CONFIGURED", "Queues are not enabled on this platform yet")
	h.wantCode(h.run("queue", "info"), 8)

	h.wantCode(h.run("queue", "list", "-o", "xml"), 2)
	h.wantCode(h.run("queue", "nonsense"), 2)
	h.wantCode(h.run("queue", "create"), 2)
}

func TestCredentialSecretIsShownOnce(t *testing.T) {
	h := signedIn(t)
	r := h.ok("queue", "credential", "create", "worker")
	contains(t, r.stdout, "test-amqp-password")
	// Listing never has it.
	absent(t, h.ok("queue", "credential", "list", "-o", "json").stdout, "password")
}

func TestQueueInfo(t *testing.T) {
	h := signedIn(t)
	h.golden("queue-info", h.ok("queue", "info").stdout)
}

func TestWebhooks(t *testing.T) {
	h := signedIn(t)
	h.golden("webhook-list", h.ok("webhook", "list").stdout)
	h.golden("webhook-get", h.ok("webhook", "get", "orders-hook").stdout)

	// The signing secret comes from standard input and is never printed.
	r := h.runIn("s3cr3t-value\n", "webhook", "create", "billing", "--topic", "events", "--filter", "invoice.*",
		"--url", "https://hooks.example.com/billing", "--secret-stdin", "--header", "X-Env=prod")
	h.wantCode(r, 0)
	absent(t, r.stdout+r.stderr, "s3cr3t-value")
	call, _ := h.api.LastCall("createQueueWebhook")
	in := call.Variables["input"].(map[string]any)
	if in["secret"] != "s3cr3t-value" || in["topic"] != "events" || in["filter"] != "invoice.*" {
		t.Errorf("createQueueWebhook input: %v", in)
	}
	if _, has := in["queue"]; has {
		t.Errorf("an empty queue was sent: %v", in)
	}

	h.wantCode(h.run("webhook", "create", "both", "--topic", "a", "--queue", "b", "--url", "https://hooks.example.com/x"), 2)
	h.wantCode(h.run("webhook", "create", "neither", "--url", "https://hooks.example.com/x"), 2)
	h.wantCode(h.run("webhook", "update", "billing"), 2)

	contains(t, h.ok("webhook", "disable", "orders-hook").stdout, "DISABLED")
	call, _ = h.api.LastCall("updateQueueWebhook")
	in = call.Variables["input"].(map[string]any)
	if in["enabled"] != false || len(in) != 2 {
		t.Errorf("disable must change only enabled: %v", in)
	}
}

func TestScheduledMessages(t *testing.T) {
	h := signedIn(t)
	h.golden("schedule-list", h.ok("schedule", "list").stdout)

	r := h.runIn(`{"kind":"report"}`, "schedule", "create", "weekly", "--cron", "0 7 * * 1", "--topic", "events",
		"--routing-key", "report.weekly", "--body", "-", "--content-type", "application/json")
	h.wantCode(r, 0)
	call, _ := h.api.LastCall("createScheduledMessage")
	in := call.Variables["input"].(map[string]any)
	if in["body"] != `{"kind":"report"}` || in["schedule"] != "0 7 * * 1" || in["enabled"] != true || in["routingKey"] != "report.weekly" {
		t.Errorf("createScheduledMessage input: %v", in)
	}
	h.wantCode(h.run("schedule", "create", "bad", "--queue", "q"), 2)
	h.wantCode(h.run("schedule", "create", "bad", "--cron", "* * * * *", "--queue", "q", "--routing-key", "x"), 2)
}

func TestPlatformEvents(t *testing.T) {
	h := signedIn(t)
	r := h.ok("events", "types")
	contains(t, r.stderr, "topic rso-events")
	h.golden("events-types", r.stdout)
}
