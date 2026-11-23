# Security policy

## Supported versions

Security fixes go into the latest minor release.

## Reporting a vulnerability

Please do not open a public issue. Use GitHub's private vulnerability reporting
("Report a vulnerability" under the Security tab of this repository) or write
to office@theorigamicorporation.com. You should get an answer within a week.

Include the `rso version` output, the operating system and steps to
reproduce. Leave out real tokens, secrets and tenant ids.

## Things to know

- `rso login` saves an access token and a refresh token in
  `credentials.json` in your configuration directory, readable by you only.
  Anyone who can read that file can act as you until you run `rso logout` or
  the session is revoked.
- Kubernetes credentials are cached for minutes in your cache directory, with
  the same permissions. Set `RSO_NO_CACHE` to keep nothing on disk.
- Secrets are read from the environment or from standard input, never from
  command line flags. A secret the platform shows once (an AMQP password, an
  S3 secret key) is printed to standard output: mind where that output goes.
- A kubeconfig made with `--session` carries a token; the default kind does
  not.
