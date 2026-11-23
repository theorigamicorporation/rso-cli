# Contributing

Thanks for helping. A bug report with the command line, what it printed and
its exit code (with tenant ids and secrets replaced) is the most useful thing
you can send.

## Setup

1. Install the pinned toolchain: `asdf install` (see `.tool-versions`). If you
   do not use asdf, install the same versions of Go, golangci-lint, just and
   goreleaser some other way.
2. Run `just` to see the recipes. CI runs the same ones.

## Day to day

```sh
just fmt        # gofmt
just lint       # golangci-lint
just test       # unit tests with -race
just golden     # rewrite the golden files after an intended output change
just cover      # coverage over internal/...
just build      # ./bin/rso
```

## The SDK

Commands call the platform through the Go SDK,
[`rsodev-go`](https://github.com/theorigamicorporation/rsodev-go) (package
`platform`), and through nothing else: sign-in, the API calls and the error
kinds all come from it. A new API call is added to the SDK first, then used
here.

While the SDK repository is private, Go needs to be told so and git needs
access to it:

```sh
go env -w GOPRIVATE=github.com/theorigamicorporation
```

## Layout

| Path | What lives there |
|---|---|
| `cmd/rso` | The entry point: signals and the exit code |
| `internal/cli` | The commands, their flags and what they print |
| `internal/config` | The saved sign-in and settings |
| `internal/output` | Table, JSON and YAML writers |
| `internal/fakerso` | An in-memory fake of the platform, for tests |

## Tests

- Tests run the real command tree against `internal/fakerso`; nothing talks
  to the platform. The fake keeps state for queues, topics, subscriptions,
  scheduled jobs and apps, and answers the rest from fixtures a test can replace with
  `Fixed`, `Handle` and `Fail`.
- What a command prints for people is pinned by golden files in
  `internal/cli/testdata`. After an intended change run `just golden` and read
  the diff before committing it.
- What a command sends is checked on the variables the fake received
  (`LastCall`), in particular that secrets go where they should and nowhere
  else.
- Use placeholder names only (`acme`, `example.com`, documentation
  addresses). The guard test (`internal/guard`) fails the build when a real tenant id,
  internal host or cluster name appears in the repository, or the name of a
  product the platform happens to be built with: the CLI describes what the
  platform offers, not what runs it.

## Adding a command

1. Use the SDK's call. If there is none, add it to the SDK with a doc comment
   that says what it does for the caller, not how the API is built.
2. Add the command to the right group in `internal/cli`, with a table view
   and the full object as `Data` for JSON and YAML.
3. Give the fake an answer in `internal/fakerso`.
4. Add a test, a golden file, a line in `README.md` and one in
   `CHANGELOG.md`.

Rules the commands follow: results on standard output and remarks on standard
error; secrets from the environment or standard input, never from a flag;
anything that deletes asks first and takes `--yes`; no command takes a
platform cluster from the user.
