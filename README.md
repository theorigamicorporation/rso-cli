# rso

The command line tool for the [RSO cloud](https://console.cloud.rso.dev):
apps, instances, PostgreSQL, object storage, registries, Kubernetes clusters,
queues and topics, scheduled jobs, ingress, DNS and access, from a terminal
or a script.

```console
$ rso login
$ rso tenant use acme
$ rso queue create orders
$ rso queue publish orders '{"id": 7}' --json
$ rso queue receive orders
PUBLISHED         TOPIC  ROUTING KEY  BODY
2026-01-02 03:04  -      orders       {"id":7}
```

## Install

Download the archive for your system from the
[releases page](https://github.com/theorigamicorporation/rso-cli/releases),
check it against the `SHA256SUMS` file of the release, and put `rso` on your
`PATH`. Archives exist for Linux, macOS and Windows on amd64 and arm64.

```sh
sha256sum --check --ignore-missing rso_*_SHA256SUMS
tar -xzf rso_*_linux_amd64.tar.gz rso
sudo install rso /usr/local/bin/rso
```

Or build it with Go:

```sh
go install github.com/theorigamicorporation/rso-cli/cmd/rso@latest
```

`rso version` prints what you have.

Shell completion: `rso completion bash|zsh|fish|powershell`.

## Signing in

| Where | How |
|---|---|
| Your own machine | `rso login` prints a link and a code; confirm it in a browser. The sign-in is saved and renewed. |
| No browser | `printf '%s' "$TOKEN" \| rso login --with-token` |
| CI and automation | Set `RSO_SERVICE_ACCOUNT_NAME` and `RSO_SERVICE_ACCOUNT_SECRET`, or `RSO_ACCESS_TOKEN`. Nothing is saved. |

Credentials from the environment win over a saved sign-in. Secrets are never
taken from command line flags, so they do not end up in shell history or in
the process list: they come from the environment or from standard input.

`rso logout` forgets the saved sign-in. `rso whoami` shows who you are.

## Choosing a tenant

Every command acts in one tenant:

```sh
rso tenant list
rso tenant use acme        # by name or id; remembered for later commands
rso queue list --tenant <id>   # for one command
export RSO_TENANT=<id>         # for a script
```

## Commands

| Group | What it does |
|---|---|
| `rso queue` | Create, list, delete and purge queues; `publish` and `receive` messages; `info`; AMQP `credential`s |
| `rso topic` | Topics, `publish`, `subscribe` a queue with a routing key filter, `subscriptions`, `unsubscribe` |
| `rso webhook` | Deliver a topic's or a queue's messages to an https URL, at least once; `disable`, `enable`, `resume` |
| `rso schedule` | Publish a fixed message on a cron schedule; `run` it now |
| `rso events types` | The platform events a tenant can subscribe to, and their topic |
| `rso job` | Containers on a cron schedule: `create`, `update`, `run`, `runs`, `logs`, `suspend`, `resume` |
| `rso bucket` | Object storage buckets, S3 access `key`s, and `notification`s into a queue or topic |
| `rso kubernetes` | List clusters, print a `kubeconfig`, and the `token` helper kubectl runs |
| `rso app` | `deploy` a container image as an app (create it or roll out a new revision), `list`, `get`, `logs`, `delete` |
| `rso instance` | Virtual machines: `create` from a template, `list`, `get`, `start`, `stop`, `reboot`, `delete`; `templates` |
| `rso postgres` | PostgreSQL clusters, their `database`s and `backup`s: `enable`, `schedule`, `now`, `restore` into a new cluster |
| `rso registry` | Container registries and short-lived `credentials` for them |
| `rso volume` | Volumes apps keep their data on |
| `rso ssh-key` | The SSH public keys put on new instances |
| `rso service` | Name something outside the app platform, such as an instance, so ingress can reach it |
| `rso ingress` | Publish a service under a hostname, with a certificate |
| `rso middleware` | Basic auth, redirects and path prefix stripping for ingress routes |
| `rso dns` | `zones` offered to the tenant and the `record`s in them |
| `rso service-account` | Accounts for automation: `create`, `rotate`, `enable`, `disable`, `delete` |
| `rso access` | Who may do what: `list`, `get`, `grant`, `revoke`, `scope` to some objects only, `roles` |
| `rso quota` | What the tenant uses against its limits |
| `rso ai` | List `models` and `chat` with one |

`rso <group> --help` has the details of each.

### Things worth knowing

- **`rso queue receive` removes what it returns.** Messages are acknowledged
  as they are handed out. Where a message must survive a crashed consumer,
  consume over AMQP or deliver with a webhook.
- **Deleting asks first.** On a terminal you are asked; in a script pass
  `--yes`, otherwise the command refuses instead of guessing.
- **Secrets are shown once.** `queue credential create`,
  `bucket key create` and `service-account create` print the secret when it
  is made; it cannot be read again.
- **Nobody names a cluster.** The platform knows where a tenant's resources
  run; no command takes a cluster of the platform as an argument.

### Kubernetes sign-in

A kubeconfig from `rso kubernetes kubeconfig <cluster>` (or from the console)
holds no secret. It tells kubectl to run

```sh
rso kubernetes token --cluster=<name> --tenant=<tenant> --api=<url>
```

which signs in with your account or service account and returns a
credential that lasts minutes. `rso` has to be on the `PATH` wherever
kubectl runs. Credentials are kept in a private cache until a minute before
they expire; set `RSO_NO_CACHE` to switch that off.

## Output and exit codes

`-o table` (the default) is for people; `-o json` and `-o yaml` are for
programs and carry every field. Results go to standard output, remarks to
standard error, so a pipe gets only the result.

| Code | Meaning |
|---|---|
| 0 | Done |
| 1 | Something else went wrong |
| 2 | The command line was wrong, or the platform refused the input |
| 3 | Not signed in, or the session expired |
| 4 | The object does not exist |
| 5 | Not allowed |
| 6 | It already exists |
| 7 | A quota is used up |
| 8 | The feature is not available, or the platform is not ready |

## Configuration

| Variable | Meaning |
|---|---|
| `RSO_TENANT` | Tenant to act in |
| `RSO_REGION` | Region of the tenant (the platform has a default) |
| `RSO_ACCESS_TOKEN` | A ready access token |
| `RSO_SERVICE_ACCOUNT_NAME`, `RSO_SERVICE_ACCOUNT_SECRET` | A service account to sign in as |
| `RSO_API_URL` | API endpoint (default `https://gateway.cloud.rso.dev/graphql`) |
| `RSO_TOKEN_URL`, `RSO_DEVICE_URL`, `RSO_CLIENT_ID` | Where and as which client to sign in. Unset, they are read from the configuration document the API endpoint publishes, then from built-in defaults |
| `RSO_CONFIG_DIR` | Where the sign-in and the chosen tenant are kept (default: `rso` under your configuration directory) |
| `RSO_CACHE_DIR`, `RSO_NO_CACHE` | The Kubernetes credential cache |

The saved sign-in is in `credentials.json` in the configuration directory,
readable by you only.

## Development

See [CONTRIBUTING.md](CONTRIBUTING.md). In short: `just test`, `just lint`.
The tests run against an in-memory fake of the platform and never touch a
real tenant.

## License

Apache License 2.0. See [LICENSE](LICENSE).
