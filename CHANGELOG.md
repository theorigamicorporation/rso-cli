# Changelog

All notable changes are listed here. The format follows
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/) and the project uses
[semantic versioning](https://semver.org/).

## Unreleased

## 0.1.0 - 2026-11-23

First release.

### Added

- Sign-in: `rso login` with a browser (device flow) or `--with-token`,
  `rso logout`, `rso whoami`; service accounts and access tokens from the
  environment for automation. The saved sign-in is renewed when it expires.
- Tenants: `rso tenant list`, `use` and `current`; `--tenant` and
  `RSO_TENANT` for one command or a script.
- Queues: `rso queue` (create, list, get, delete, purge, publish, receive,
  info) and AMQP credentials (`rso queue credential`).
- Topics and subscriptions: `rso topic` (create, list, delete, publish,
  subscribe, subscriptions, unsubscribe).
- Webhooks: `rso webhook` (create, list, get, update, enable, disable,
  resume, delete).
- Scheduled messages: `rso schedule` (create, list, get, update, run,
  delete). Platform event types: `rso events types`.
- Scheduled jobs: `rso job` (create, list, get, update, run, runs, logs,
  suspend, resume, delete).
- Object storage: `rso bucket` (create, list, delete), S3 access keys
  (`rso bucket key`) and notifications (`rso bucket notification`).
- Kubernetes: `rso kubernetes` (list, get, kubeconfig) and the `token`
  credential helper that kubeconfigs run, with a private credential cache.
- Apps: `rso app` (deploy, list, get, logs, delete).
- Instances: `rso instance` (create, list, get, start, stop, reboot, delete,
  templates) and SSH keys (`rso ssh-key`).
- PostgreSQL: `rso postgres` (create, list, get, delete), databases
  (`rso postgres database`) and backups (`rso postgres backup`: enable,
  schedule, unschedule, now, restore).
- Container registries (`rso registry`, with short-lived credentials) and
  volumes (`rso volume`).
- Ingress: external services (`rso service`), routes (`rso ingress`) and
  middlewares (`rso middleware`).
- DNS: `rso dns zones` and `rso dns record` (create, list, get, update,
  delete).
- Access: service accounts (`rso service-account`) and who may do what
  (`rso access`: list, get, grant, revoke, scope, roles, resource-types).
- `rso quota`, `rso ai models`, `rso ai chat`, `rso version`, shell
  completion.
- Sign-in settings are read from the configuration document of the API
  endpoint; `RSO_TOKEN_URL`, `RSO_DEVICE_URL` and `RSO_CLIENT_ID` override
  them.
- Release archives for Linux, macOS and Windows on amd64 and arm64, with a
  `SHA256SUMS` file.
- Output as a table, JSON or YAML (`-o`), and exit codes that tell a missing
  object, a refusal, a conflict and a used-up quota apart.
