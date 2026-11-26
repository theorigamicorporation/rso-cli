package fakerso

// page wraps a list the way the paged list operations answer.
func page(items ...map[string]any) map[string]any {
	return map[string]any{"total": len(items), "items": items}
}

// echo answers a create with what was sent, on top of defaults.
func echo(defaults map[string]any) Handler {
	return func(v map[string]any) (any, *Error) {
		out := map[string]any{}
		for key, value := range defaults {
			out[key] = value
		}
		for key, value := range input(v) {
			out[key] = value
		}
		return out, nil
	}
}

// resources registers apps, instances, databases, registries, networking,
// DNS and access.
func (s *Server) resources() {
	const created = "2026-01-02T03:04:05Z"

	// Apps keep state, so a deploy can tell a new app from a new revision.
	s.apps["web"] = map[string]any{
		"name": "web", "creationTimestamp": created, "metadata": map[string]any{"labels": []any{}, "annotations": []any{}},
		"status": map[string]any{"url": "https://web.apps.example.com", "latestReadyRevisionName": "web-00007", "lastUpdatedTimestamp": "2026-01-03T10:00:00Z"},
		"template": map[string]any{"spec": map[string]any{"containers": []any{map[string]any{
			"image": "registry.example.com/acme/web:1.0", "ports": []any{map[string]any{"containerPort": 8080}},
			"env": []any{map[string]any{"name": "LOG_LEVEL", "value": "info"}},
		}}}},
	}
	s.Handle("apps", func(map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		return map[string]any{"items": sorted(s.apps)}, nil
	})
	s.Handle("app", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		app, ok := s.apps[str(v, "name")]
		if !ok {
			return nil, notFound("App", str(v, "name"))
		}
		return app, nil
	})
	save := func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		in := input(v)
		name, _ := in["name"].(string)
		app := map[string]any{"name": name, "creationTimestamp": created, "metadata": map[string]any{},
			"status":   map[string]any{"url": "https://" + name + ".apps.example.com", "latestReadyRevisionName": name + "-00001", "lastUpdatedTimestamp": created},
			"template": in["template"]}
		s.apps[name] = app
		return app, nil
	}
	s.Handle("createApp", save)
	s.Handle("updateApp", save)
	s.Handle("deleteApp", func(v map[string]any) (any, *Error) {
		s.mu.Lock()
		defer s.mu.Unlock()
		if _, ok := s.apps[str(v, "name")]; !ok {
			return nil, notFound("App", str(v, "name"))
		}
		delete(s.apps, str(v, "name"))
		return true, nil
	})
	s.Fixed("appLogs", map[string]any{"truncated": true, "lines": []map[string]any{
		{"timestamp": "2026-01-02T03:04:06Z", "pod": "web-00007-x", "container": "app", "message": "listening on :8080"},
	}})

	instance := map[string]any{"vmid": 4101, "name": "build-1", "status": "running", "cpus": 2, "maxmem": 4294967296, "maxdisk": 32212254720,
		"uptime": 3600, "config": map[string]any{"cores": 2, "memory": "4096", "description": nil},
		"networkInterfaces": map[string]any{"items": []map[string]any{{"name": "eth0", "ipAddresses": []map[string]any{{"ipAddress": "203.0.113.20", "ipAddressType": "ipv4", "prefix": 24}}}}}}
	s.Fixed("instances", map[string]any{"items": []map[string]any{instance}})
	s.Fixed("instance", instance)
	s.Fixed("instanceTemplates", map[string]any{"items": []map[string]any{{"id": "901", "name": "ubuntu-26.04", "description": "Ubuntu 26.04 LTS"}}})
	s.Fixed("createInstance", "task-1")
	s.Fixed("taskProgress", nil)
	for _, field := range []string{"startInstance", "stopInstance", "rebootInstance", "deleteInstance"} {
		s.Fixed(field, true)
	}

	postgres := map[string]any{"name": "orders", "creationTimestamp": created,
		"spec":   map[string]any{"instances": 2, "storage": map[string]any{"size": "20Gi"}, "resources": nil},
		"status": map[string]any{"phase": "healthy", "url": "postgresql://orders-rw:5432", "instances": 2, "readyInstances": 2}}
	s.Fixed("postgresClustersByCluster", map[string]any{"items": []map[string]any{postgres}})
	s.Fixed("postgresClusterByCluster", postgres)
	s.Handle("createPostgresCluster", func(v map[string]any) (any, *Error) {
		in := input(v)
		return map[string]any{"name": in["name"], "creationTimestamp": created, "spec": in["spec"], "status": nil}, nil
	})
	s.Fixed("deletePostgresCluster", true)
	s.Fixed("postgresDatabasesByCluster", map[string]any{"items": []map[string]any{{"clusterName": "orders", "name": "shop", "owner": "app", "databaseReclaimPolicy": "retain"}}})
	s.Handle("createPostgresDatabase", echo(map[string]any{"databaseReclaimPolicy": "retain"}))
	s.Fixed("deletePostgresDatabase", true)
	backups := map[string]any{"available": true, "enabled": true, "retention": "30d",
		"schedule": map[string]any{"schedule": "0 3 * * *", "suspended": false, "lastScheduleTime": created, "nextScheduleTime": "2026-01-03T03:00:00Z"},
		"window":   map[string]any{"continuousArchiving": true, "from": created, "to": "2026-01-02T09:00:00Z"},
		"backups":  []map[string]any{{"name": "orders-20260102", "phase": "completed", "scheduled": true, "startedAt": created, "stoppedAt": "2026-01-02T03:06:00Z", "error": nil}}}
	for _, field := range []string{"postgresBackupOverview", "enablePostgresBackups", "setPostgresBackupSchedule"} {
		s.Fixed(field, backups)
	}
	s.Fixed("removePostgresBackupSchedule", true)
	s.Fixed("backupPostgresCluster", map[string]any{"name": "orders-manual", "phase": "running", "scheduled": false, "startedAt": created, "stoppedAt": nil, "error": nil})
	s.Handle("restorePostgresCluster", func(v map[string]any) (any, *Error) {
		return map[string]any{"name": input(v)["name"], "creationTimestamp": created, "spec": postgres["spec"], "status": nil}, nil
	})

	registry := map[string]any{"name": "images", "domain": "images.registry.example.com", "status": "READY", "createdAt": created}
	s.Fixed("dockerRegistries", page(registry))
	s.Fixed("dockerRegistry", registry)
	s.Handle("createDockerRegistry", echo(map[string]any{"domain": "new.registry.example.com", "status": "PENDING", "createdAt": created}))
	s.Fixed("deleteDockerRegistry", true)
	s.Fixed("issueDockerRegistryCredentials", map[string]any{"registry": "images.registry.example.com", "username": "robot-1", "password": "test-registry-password",
		"expiresAt": "2026-01-02T04:04:05Z", "pullSecretName": nil})

	key := map[string]any{"id": "key-1", "name": "laptop", "publicKey": "ssh-ed25519 AAAAC3Nza test", "fingerprint": "SHA256:abc", "keyType": "ssh-ed25519",
		"comment": "test", "scope": "TENANT", "createdAt": created, "createdBy": "ada", "usable": true}
	s.Fixed("sshKeys", []map[string]any{key})
	s.Handle("addSshKey", echo(key))
	s.Fixed("deleteSshKey", true)

	volume := map[string]any{"id": "vol-1", "name": "uploads", "sizeGb": 10, "status": "Bound", "usedBy": []string{"web"}, "deletable": false, "createdAt": created}
	s.Fixed("volumes", map[string]any{"items": []map[string]any{volume}})
	s.Handle("createVolume", echo(map[string]any{"id": "vol-2", "status": "Pending", "usedBy": []string{}, "deletable": true, "createdAt": created}))
	s.Fixed("deleteVolume", true)

	service := map[string]any{"name": "shop", "externalName": "shop.hosts.example.com", "ports": []map[string]any{{"name": nil, "port": 80, "targetPort": 8080, "protocol": "TCP"}}}
	s.Fixed("externalServices", page(service))
	s.Fixed("externalService", service)
	s.Handle("createExternalService", echo(nil))
	s.Fixed("deleteExternalService", true)

	route := map[string]any{"name": "shop", "entryPoints": []string{"https"}, "certResolver": "default", "routes": []map[string]any{{
		"match": "Host(`shop.example.com`)", "middlewares": []map[string]any{{"name": "staff"}},
		"services": []map[string]any{{"name": "shop", "port": 80, "scheme": nil, "serversTransport": nil}}}}}
	s.Fixed("ingressRoutes", page(route))
	s.Fixed("ingressRoute", route)
	s.Handle("createIngressRoute", echo(map[string]any{"entryPoints": []string{"https"}, "certResolver": "default"}))
	s.Fixed("deleteIngressRoute", true)

	middleware := map[string]any{"name": "strip-api", "type": "stripPrefix", "redirectRegex": nil, "redirectScheme": nil, "stripPrefix": map[string]any{"prefixes": []string{"/api"}}}
	s.Fixed("middlewares", page(middleware))
	s.Fixed("middleware", middleware)
	s.Handle("createMiddleware", func(v map[string]any) (any, *Error) {
		in := input(v)
		return map[string]any{"name": in["name"], "type": in["type"], "redirectRegex": in["redirectRegex"], "redirectScheme": in["redirectScheme"], "stripPrefix": in["stripPrefix"]}, nil
	})
	s.Fixed("deleteMiddleware", true)

	record := map[string]any{"id": "33333333-3333-4333-8333-333333333333", "zone": "example.com", "name": "shop.example.com", "type": "A", "content": "203.0.113.10",
		"createdAt": created, "createdBy": "ada", "updatedAt": created}
	s.Fixed("dnsAvailable", true)
	zone := map[string]any{"name": "example.com", "status": "ACTIVE", "nameservers": []string{}, "delegated": false, "createdAt": nil}
	ownZone := map[string]any{"name": "example.org", "status": "PENDING", "nameservers": []string{"ns1.example.net", "ns2.example.net"}, "delegated": true, "createdAt": created}
	s.Fixed("dnsZones", []map[string]any{zone, ownZone})
	s.Fixed("dnsZone", ownZone)
	s.Handle("createDnsZone", func(v map[string]any) (any, *Error) {
		out := map[string]any{}
		for k, value := range ownZone {
			out[k] = value
		}
		out["name"] = v["domain"]
		return out, nil
	})
	s.Fixed("deleteDnsZone", true)

	exposure := map[string]any{"host": "shop.example.org", "namespace": TenantA, "status": "ACTIVE", "tls": "AUTO", "dnsManaged": false,
		"dnsTarget": []string{"edge.example.net"}, "domain": "example.org", "domainVerified": true,
		"target": map[string]any{"address": "198.51.100.10", "port": 8443, "scheme": "HTTPS", "insecureSkipVerify": true}, "targetOutOfRange": false,
		"proxied": false, "replacedRecords": []any{}, "managedByPlatform": false,
		"basicAuthUser": nil, "createdAt": created, "createdBy": "ada", "updatedAt": created}
	s.Fixed("exposuresAvailable", true)
	s.Fixed("exposures", page(exposure))
	s.Fixed("exposure", exposure)
	s.Handle("exposeHost", func(v map[string]any) (any, *Error) {
		in := input(v)
		out := map[string]any{}
		for k, value := range exposure {
			out[k] = value
		}
		out["host"], out["target"], out["status"] = in["host"], in["target"], "PENDING"
		if tls, ok := in["tls"].(string); ok {
			out["tls"] = tls
		}
		if auth, ok := in["basicAuth"].(map[string]any); ok {
			out["basicAuthUser"] = auth["username"]
		}
		return out, nil
	})
	s.Fixed("unexposeHost", true)

	domain := map[string]any{"domain": "example.org", "status": "PENDING", "challengeName": "_platform-challenge.example.org", "challengeValue": "verify-0123456789abcdef",
		"verifiedAt": nil, "lastCheckedAt": nil, "createdAt": created}
	s.Fixed("domainsAvailable", true)
	s.Fixed("domains", []map[string]any{domain})
	s.Handle("registerDomain", func(v map[string]any) (any, *Error) {
		out := map[string]any{}
		for k, value := range domain {
			out[k] = value
		}
		out["domain"] = v["domain"]
		return out, nil
	})
	s.Handle("verifyDomain", func(v map[string]any) (any, *Error) {
		out := map[string]any{}
		for k, value := range domain {
			out[k] = value
		}
		out["domain"], out["status"], out["verifiedAt"], out["lastCheckedAt"] = v["domain"], "VERIFIED", created, created
		return out, nil
	})
	s.Fixed("removeDomain", true)
	s.Fixed("dnsRecords", []map[string]any{record})
	s.Fixed("dnsRecord", record)
	s.Handle("createDnsRecord", echo(map[string]any{"id": "44444444-4444-4444-8444-444444444444", "createdAt": created, "createdBy": "ada", "updatedAt": created}))
	s.Handle("updateDnsRecord", func(v map[string]any) (any, *Error) {
		out := map[string]any{}
		for k, value := range record {
			out[k] = value
		}
		out["content"] = input(v)["content"]
		return out, nil
	})
	s.Fixed("deleteDnsRecord", true)

	account := map[string]any{"id": "sa-1", "name": "ci-deployer", "status": "ACTIVE", "isActive": true, "expiresAt": "2026-04-02T03:04:05Z"}
	s.Fixed("serviceAccounts", []map[string]any{account})
	s.Handle("createServiceAccount", func(v map[string]any) (any, *Error) {
		in := input(v)
		return map[string]any{"token": "test-new-service-secret", "serviceAccount": map[string]any{"id": "sa-2", "name": in["name"], "status": "ACTIVE", "isActive": true, "expiresAt": in["expiresAt"]}}, nil
	})
	s.Fixed("rotateServiceAccountToken", map[string]any{"token": "test-rotated-service-secret", "serviceAccount": account})
	s.Fixed("setServiceAccountActive", account)
	s.Fixed("deleteServiceAccount", true)

	principal := func(scoped []any) map[string]any {
		return map[string]any{"id": "p-1", "username": "ci-deployer", "name": "CI deployer", "email": nil, "type": "SERVICE_ACCOUNT", "restricted": true,
			"roles": []string{}, "permissions": []string{"apps.read"}, "effectivePermissions": []string{"apps.read"}, "scoped": scoped}
	}
	existing := []any{map[string]any{"resourceType": "bucket", "roles": []string{}, "permissions": []string{"storage.objects.read"},
		"selector": map[string]any{"names": []string{"media"}, "prefixes": []string{}, "labels": []any{}}}}
	s.Fixed("iamPrincipals", []map[string]any{principal(existing)})
	s.Handle("setPrincipalAccess", func(v map[string]any) (any, *Error) {
		scoped, _ := input(v)["scoped"].([]any)
		return principal(scoped), nil
	})
	s.Fixed("grantPermissions", principal(existing))
	s.Fixed("revokePermissions", principal(existing))
	s.Fixed("iamRoles", []map[string]any{{"name": "EDITOR", "description": "Change resources", "builtIn": true, "permissions": []string{"apps.read", "apps.write"}}})
	s.Fixed("resourceTypes", []map[string]any{{"name": "app"}, {"name": "bucket"}})
}
