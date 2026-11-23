package fakerso

import "fmt"

func str(variables map[string]any, key string) string {
	value, _ := variables[key].(string)
	return value
}

func input(variables map[string]any) map[string]any {
	value, _ := variables["input"].(map[string]any)
	return value
}

func notFound(what, name string) *Error {
	return &Error{Code: "NOT_FOUND", Message: fmt.Sprintf("%s %s not found", what, name)}
}

// defaults registers the answers of a small, working tenant.
func (s *Server) defaults() {
	s.Fixed("myUser", map[string]any{
		"id": "u-1", "username": "ada", "name": "Ada Example", "email": "ada@example.com", "platformRoles": []string{},
		"tenants": []map[string]any{
			{"id": TenantA, "name": "acme", "region": "REGION_ONE", "myRoles": []string{"ADMIN"}},
			{"id": TenantB, "name": "globex", "region": "REGION_TWO", "myRoles": []string{"VIEWER"}},
		},
	})
	s.Fixed("quota", []map[string]any{
		{"name": "vcpu", "used": 4, "limit": 16},
		{"name": "apps", "used": 1, "limit": 20},
		{"name": "queues", "used": 2, "limit": 20},
		{"name": "aiTokens", "used": 1250000, "limit": 5000000},
	})
	s.Fixed("queuesInfo", map[string]any{
		"available": true, "httpEndpoint": s.URL + "/queues/v1", "amqpHost": "queues.internal.example", "amqpPort": 5672,
		"vhost": "tenant-acme", "maxMessageBytes": 262144, "maxQueueLength": 100000, "maxQueueBytes": 67108864,
		"messageRetentionSeconds": 604800, "maxReceive": 32,
	})

	// Queues, with the depth the data routes produce.
	queue := func(name string) map[string]any {
		waiting := len(s.messages[name])
		return map[string]any{"name": name, "status": "READY", "statusReason": nil, "createdAt": "2026-01-02T03:04:05Z",
			"messages": waiting, "messagesReady": waiting, "messagesUnacknowledged": 0, "consumers": 0}
	}
	s.Handle("queues", func(map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		for _, q := range sorted(s.queues) {
			out = append(out, queue(q["name"].(string)))
		}
		return out, nil
	})
	s.Handle("queue", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.queues[str(v, "name")]; !ok {
			return nil, notFound("Queue", str(v, "name"))
		}
		return queue(str(v, "name")), nil
	})
	s.Handle("createQueue", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		name := str(v, "name")
		if _, ok := s.queues[name]; ok {
			return nil, &Error{Code: "CONFLICT", Message: "Queue " + name + " already exists"}
		}
		s.queues[name] = map[string]any{"name": name}
		return queue(name), nil
	})
	s.Handle("deleteQueue", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.queues[str(v, "name")]; !ok {
			return nil, notFound("Queue", str(v, "name"))
		}
		delete(s.queues, str(v, "name"))
		delete(s.messages, str(v, "name"))
		return true, nil
	})
	s.Handle("purgeQueue", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.messages, str(v, "name"))
		return true, nil
	})

	topic := func(name string) map[string]any {
		subscriptions := 0
		for _, sub := range s.subs {
			if sub["topic"] == name {
				subscriptions++
			}
		}
		return map[string]any{"name": name, "status": "READY", "statusReason": nil, "createdAt": "2026-01-02T03:04:05Z", "subscriptionCount": subscriptions}
	}
	s.Handle("topics", func(map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		for _, t := range sorted(s.topics) {
			out = append(out, topic(t["name"].(string)))
		}
		return out, nil
	})
	s.Handle("createTopic", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		s.topics[str(v, "name")] = map[string]any{"name": str(v, "name")}
		return topic(str(v, "name")), nil
	})
	s.Handle("deleteTopic", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.topics, str(v, "name"))
		return true, nil
	})
	s.Handle("topicSubscriptions", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		out := []map[string]any{}
		for _, sub := range s.subs {
			if t := str(v, "topic"); t != "" && sub["topic"] != t {
				continue
			}
			out = append(out, sub)
		}
		return out, nil
	})
	s.Handle("createTopicSubscription", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		in := input(v)
		filter, _ := in["filter"].(string)
		if filter == "" {
			filter = "#"
		}
		sub := map[string]any{"id": fmt.Sprintf("sub%09d", len(s.subs)+1), "topic": in["topic"], "filter": filter,
			"target": map[string]any{"queue": in["queue"]}, "status": "READY", "statusReason": nil, "createdAt": "2026-01-02T03:04:05Z"}
		s.subs = append(s.subs, sub)
		return sub, nil
	})
	s.Handle("deleteTopicSubscription", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		for i, sub := range s.subs {
			if sub["id"] == str(v, "id") {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				return true, nil
			}
		}
		return nil, notFound("Subscription", str(v, "id"))
	})
	s.Fixed("queueCredentials", []map[string]any{{"name": "worker", "status": "READY", "createdAt": "2026-01-02T03:04:05Z"}})
	s.Handle("createQueueCredential", func(v map[string]any) (any, *Error) {
		return map[string]any{"name": str(v, "name"), "username": "tenant-acme-" + str(v, "name"), "password": "test-amqp-password",
			"host": "queues.internal.example", "port": 5672, "vhost": "tenant-acme",
			"uri": "amqp://tenant-acme-" + str(v, "name") + ":test-amqp-password@queues.internal.example:5672/tenant-acme", "createdAt": "2026-01-02T03:04:05Z"}, nil
	})
	s.Fixed("deleteQueueCredential", true)

	// Scheduled jobs keep their definition, so update can be checked.
	job := func(in map[string]any) map[string]any {
		out := map[string]any{"command": []string{}, "args": []string{}, "env": []any{}, "imagePullSecrets": []string{},
			"concurrency": "FORBID", "timeoutSeconds": 3600, "retries": 2, "suspended": false,
			"createdAt": "2026-01-02T03:04:05Z", "nextRunAt": "2026-01-02T04:00:00Z", "lastSuccessfulAt": nil, "activeRuns": 0, "lastRun": nil}
		for key, value := range in {
			out[key] = value
		}
		return out
	}
	s.Handle("scheduledJobs", func(map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		return map[string]any{"items": sorted(s.jobs)}, nil
	})
	s.Handle("scheduledJob", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if j, ok := s.jobs[str(v, "name")]; ok {
			return j, nil
		}
		return nil, notFound("Scheduled job", str(v, "name"))
	})
	save := func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		in := input(v)
		name, _ := in["name"].(string)
		s.jobs[name] = job(in)
		return s.jobs[name], nil
	}
	s.Handle("createScheduledJob", save)
	s.Handle("updateScheduledJob", save)
	s.Handle("setScheduledJobSuspended", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		j, ok := s.jobs[str(v, "name")]
		if !ok {
			return nil, notFound("Scheduled job", str(v, "name"))
		}
		j["suspended"] = v["suspended"]
		return j, nil
	})
	run := map[string]any{"name": "nightly-manual-abc123", "status": "SUCCEEDED", "manual": true, "startedAt": "2026-01-02T03:04:05Z",
		"finishedAt": "2026-01-02T03:04:10Z", "durationSeconds": 5, "failedAttempts": 0, "message": nil}
	s.Fixed("runScheduledJob", run)
	s.Fixed("scheduledJobRuns", []map[string]any{run})
	s.Fixed("scheduledJobLogs", map[string]any{"run": "nightly-manual-abc123", "truncated": false, "lines": []map[string]any{
		{"timestamp": "2026-01-02T03:04:06Z", "pod": "nightly-manual-abc123-x", "container": "job", "message": "starting"},
		{"timestamp": "2026-01-02T03:04:09Z", "pod": "nightly-manual-abc123-x", "container": "job", "message": "done"},
	}})
	s.Handle("deleteScheduledJob", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.jobs, str(v, "name"))
		return true, nil
	})

	webhook := func(in map[string]any) map[string]any {
		out := map[string]any{"topic": nil, "filter": nil, "queue": nil, "hasSecret": in["secret"] != nil, "headerNames": []string{},
			"enabled": true, "state": "ACTIVE", "pauseReason": nil, "consecutiveFailures": 0, "lastStatus": 200, "lastError": nil,
			"lastAttemptAt": "2026-01-02T03:04:05Z", "lastSuccessAt": "2026-01-02T03:04:05Z", "deliveredCount": 12, "deadLetteredCount": 0,
			"deadLetterQueue": fmt.Sprintf("rso-wh-%v-dead", in["name"]), "messagesWaiting": 0, "messagesDeadLettered": 0, "createdAt": "2026-01-02T03:04:05Z"}
		for key, value := range in {
			if key != "secret" && key != "headers" {
				out[key] = value
			}
		}
		return out
	}
	orders := webhook(map[string]any{"name": "orders-hook", "topic": "events", "filter": "order.*", "url": "https://hooks.example.com/orders"})
	s.Fixed("queueWebhooks", []map[string]any{orders})
	s.Fixed("queueWebhook", orders)
	s.Handle("createQueueWebhook", func(v map[string]any) (any, *Error) { return webhook(input(v)), nil })
	s.Handle("updateQueueWebhook", func(v map[string]any) (any, *Error) {
		out := webhook(map[string]any{"name": input(v)["name"], "topic": "events", "url": "https://hooks.example.com/orders"})
		for key, value := range input(v) {
			if key == "url" || key == "enabled" {
				out[key] = value
			}
		}
		if out["enabled"] == false {
			out["state"] = "DISABLED"
		}
		return out, nil
	})
	s.Fixed("resumeQueueWebhook", orders)
	s.Fixed("deleteQueueWebhook", true)
	s.Fixed("platformEvents", map[string]any{"available": true, "topic": "rso-events", "types": []map[string]any{
		{"type": "apps.app.created", "description": "An app was created"},
		{"type": "instances.instance.started", "description": "An instance was started"},
		{"type": "queues.queue.deleted", "description": "A queue was deleted"},
	}})

	scheduled := func(in map[string]any) map[string]any {
		out := map[string]any{"queue": nil, "topic": nil, "routingKey": nil, "contentType": nil, "enabled": true,
			"nextRunAt": "2026-01-05T06:00:00Z", "lastRunAt": nil, "lastStatus": nil, "lastError": nil, "runCount": 0, "createdAt": "2026-01-02T03:04:05Z"}
		for key, value := range in {
			out[key] = value
		}
		return out
	}
	digest := scheduled(map[string]any{"name": "digest", "schedule": "0 6 * * 1-5", "queue": "reports", "body": `{"kind":"digest"}`})
	s.Fixed("scheduledMessages", []map[string]any{digest})
	s.Fixed("scheduledMessage", digest)
	s.Handle("createScheduledMessage", func(v map[string]any) (any, *Error) { return scheduled(input(v)), nil })
	s.Handle("updateScheduledMessage", func(v map[string]any) (any, *Error) { return scheduled(input(v)), nil })
	s.Fixed("runScheduledMessage", digest)
	s.Fixed("deleteScheduledMessage", true)

	bucket := func(name string) map[string]any {
		return map[string]any{"name": name, "bucket": "t0a1b2c3d-" + name, "createdAt": "2026-01-02T03:04:05Z", "sizeBytes": 5368709120,
			"objectCount": 1200, "endpoint": "https://s3.example.com", "region": "region-one"}
	}
	s.Fixed("buckets", []map[string]any{bucket("media"), bucket("uploads")})
	s.Handle("createBucket", func(v map[string]any) (any, *Error) { return bucket(str(v, "name")), nil })
	s.Fixed("deleteBucket", true)
	s.Fixed("objectStorageKeys", []map[string]any{{"name": "backup", "accessKeyId": "AKIAEXAMPLE", "createdAt": "2026-01-02T03:04:05Z"}})
	s.Handle("createObjectStorageKey", func(v map[string]any) (any, *Error) {
		return map[string]any{"name": str(v, "name"), "accessKeyId": "AKIAEXAMPLE", "secretAccessKey": "test-secret-access-key", "createdAt": "2026-01-02T03:04:05Z"}, nil
	})
	s.Fixed("deleteObjectStorageKey", true)
	notification := func(in map[string]any) map[string]any {
		out := map[string]any{"id": "n-1", "keyPrefix": "", "keySuffix": "", "queue": nil, "topic": nil, "routingKey": nil,
			"createdAt": "2026-01-02T03:04:05Z", "delivered": 3, "dropped": 0, "lastDeliveredAt": nil, "lastError": nil}
		for key, value := range in {
			out[key] = value
		}
		return out
	}
	s.Fixed("bucketNotifications", []map[string]any{notification(map[string]any{"bucket": "uploads", "events": []string{"CREATED"}, "keyPrefix": "incoming/", "keySuffix": ".csv", "queue": "imports"})})
	s.Handle("createBucketNotification", func(v map[string]any) (any, *Error) { return notification(input(v)), nil })
	s.Fixed("deleteBucketNotification", true)

	cluster := map[string]any{"name": "production", "status": "READY", "statusReason": nil, "version": "1.37.2", "targetVersion": nil,
		"endpoint": "https://192.0.2.10:6443", "createdAt": "2026-01-02T03:04:05Z",
		"controlPlane": map[string]any{"replicas": 3, "readyReplicas": 3, "vcpu": 2, "ramGb": 4, "diskGb": 30},
		"pools":        []map[string]any{{"name": "workers", "replicas": 2, "readyReplicas": 1, "vcpu": 4, "ramGb": 8, "diskGb": 50}}}
	s.Fixed("tenantClusters", map[string]any{"items": []map[string]any{cluster}})
	s.Fixed("tenantCluster", cluster)
	s.Fixed("tenantClusterKubeconfig", "apiVersion: v1\nkind: Config\n")
	s.Fixed("tenantClusterToken", map[string]any{"execCredential": `{"apiVersion":"client.authentication.k8s.io/v1","kind":"ExecCredential","status":{"token":"test-cluster-token","expirationTimestamp":"2999-01-01T00:00:00Z"}}`})

	s.Fixed("aiModels", map[string]any{"items": []map[string]any{
		{"name": "small-model:latest", "size": 4294967296, "status": "READY", "loaded": true, "capabilities": []string{"completion"}},
	}})

	s.resources()
}
