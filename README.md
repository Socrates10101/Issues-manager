# issues-manager

A small, self-hosted issue tracker whose source of truth is a directory of
markdown files. A thin HTTP API and a CLI (`issues`) sit on top of that
directory so you can list, create, update and archive issues from any machine,
while the data stays readable with `cat` and `grep` over ssh. There is no
database, no cloud account, and no built-in user management: it is meant to run
on a trusted private network (home lab, VPN, LAN) for one person or a small
team.

- **Markdown is the data.** One file per issue, with a short frontmatter block.
  Back it up, grep it, or edit it by hand.
- **Thin server.** A single Go binary with zero third-party dependencies serves
  a JSON API over the directory.
- **CLI included.** The same binary is the client. Point it at the server with
  one environment variable.
- **Optional bearer token.** Enough to keep casual traffic out; not a substitute
  for network isolation.
- **NixOS module.** `services.issues.enable = true` and you are running.

## Install / build

Requires Go 1.23 or newer.

```sh
go build -o issues ./cmd/issues
# or
go install github.com/Socrates10101/issues-manager/cmd/issues@latest
```

With Nix (flakes):

```sh
nix build github:Socrates10101/Issues-manager   # ./result/bin/issues
nix run   github:Socrates10101/Issues-manager -- help
```

The flake exposes `packages.<system>.default` (alias `issues`),
`nixosModules.default` (alias `issues`) and a `devShells.default` with Go.

## Running the server

```sh
issues serve -data /var/lib/issues -addr 127.0.0.1 -port 8787
```

| Flag          | Env var             | Default           | Meaning                                  |
|---------------|---------------------|-------------------|------------------------------------------|
| `-data`       | `ISSUES_DATA`       | `/var/lib/issues` | Data directory (created if missing)      |
| `-addr`       | `ISSUES_ADDR`       | `0.0.0.0`         | Bind address                             |
| `-port`       | `ISSUES_PORT`       | `8787`            | Listen port                              |
| `-token-file` | `ISSUES_TOKEN_FILE` | (none)            | File containing a static bearer token    |

Flags override environment variables. When a token file is given, every
request must carry `Authorization: Bearer <token>`; otherwise the API is open to
anyone who can reach the port. The server shuts down cleanly on `SIGINT` /
`SIGTERM`.

## Data directory and file format

```
<data>/<project>/<NNN>-<slug>.md            active issues
<data>/archive/<project>/<NNN>-<slug>.md    archived issues
```

- A *project* is just a sub-directory name. Projects are created on first use.
- Issues are numbered per project, starting at 1. The next number is always
  one more than the highest number in either the active or archive directory,
  so numbers are never reused, even after archiving.
- The identity of an issue is `project:NNN` (for example `infra:008`). The slug
  is derived from the title (lower-cased, unsafe characters replaced by `-`,
  truncated to 60 bytes, non-ASCII kept). Renaming an issue renames the file.
- Archiving moves the file from `<project>/` to `archive/<project>/`.
- Writes go to a `.tmp` file in the same directory followed by a rename.
- The directory name `archive` is reserved and cannot be used as a project.

Each file looks like this:

```markdown
---
project: infra
number: 8
title: Add WAF rules at the edge
status: 新規
created: 2026-06-28
updated: 2026-06-28
---

Free-form markdown body. Anything after the frontmatter is stored verbatim.
```

The frontmatter is a strict `key: value` subset (no nesting, no quoting), so
the server needs no YAML library. Unknown keys are ignored on read and dropped
on the next write. If `number:` is missing, the number is taken from the
filename.

### Statuses

The lifecycle has five fixed statuses. The strings are Japanese and are part of
the on-disk and API contract:

| Status              | Meaning                      |
|---------------------|------------------------------|
| `新規`              | new                          |
| `調査中`            | investigating                |
| `対応中`            | in progress                  |
| `修正完了・検証待ち` | fixed, awaiting verification |
| `解決済`            | resolved                     |

New issues start as `新規`. Updates with any other string are rejected with
HTTP 400. Only issues in `解決済` can be archived; the server answers 409
otherwise. Archiving is idempotent.

## CLI

The client reads `ISSUES_URL` (default `http://127.0.0.1:8787`) and, if set,
`ISSUES_TOKEN`.

```sh
export ISSUES_URL=http://issues.internal:8787
export ISSUES_TOKEN=$(cat ~/.config/issues/token)   # only if the server uses one

issues new "Add WAF rules at the edge" -p infra
# created infra:001 — Add WAF rules at the edge

issues ls                      # active issues, all projects
issues ls -p infra             # one project
issues ls -s 対応中            # by status
issues ls --archived           # only archived
issues ls --all                # active + archived
# ID         STATUS  A  TITLE
# infra:001  新規       Add WAF rules at the edge
#
# 1 issues

issues show infra:1            # numbers may be given with or without zero padding
issues status infra:1 "対応中"
issues status infra:1 "解決済"
issues archive infra:1
# archived infra:001
```

`issues ls` is also available as `issues list`. The title for `issues new` may
come before or after the `-p` flag.

## HTTP API

All responses are JSON. Errors are `{"error": "..."}` with an appropriate
status code. If a token is configured, every route (including `/healthz`)
requires `Authorization: Bearer <token>` and answers 401 otherwise.

| Method / path                              | Body                        | Result                                          |
|--------------------------------------------|-----------------------------|-------------------------------------------------|
| `GET /healthz`                             |                             | `{"status":"ok"}`                               |
| `GET /projects`                            |                             | `[{"name":..., "count":...}]` (active + archived) |
| `GET /issues?project=&status=&archived=`   |                             | array of issues; `archived` is `true`/`false`, omit for both |
| `POST /issues`                             | `{"project","title","body"?}` | 201 + issue; 400 if project or title missing  |
| `GET /issues/{project}/{number}`           |                             | issue; 404 if unknown                           |
| `PATCH /issues/{project}/{number}`         | `{"title"?,"status"?,"body"?}` | updated issue; 400 on invalid status; 404      |
| `POST /issues/{project}/{number}/archive`  |                             | archived issue; 409 unless status is `解決済`; 404 |

An issue object:

```json
{
  "project": "infra",
  "number": 1,
  "title": "Add WAF rules at the edge",
  "status": "新規",
  "created": "2026-06-28",
  "updated": "2026-06-28",
  "body": "Free-form markdown body.\n",
  "archived": false
}
```

Example with curl:

```sh
curl -s -X POST "$ISSUES_URL/issues" \
  -H 'Content-Type: application/json' \
  -d '{"project":"infra","title":"Add WAF rules at the edge"}'

curl -s -X PATCH "$ISSUES_URL/issues/infra/1" \
  -H 'Content-Type: application/json' \
  -d '{"status":"解決済"}'
```

## NixOS module

Add the flake as an input, import the module, and enable the service:

```nix
# flake.nix
inputs.issues-manager.url = "github:Socrates10101/Issues-manager";

# configuration.nix (or a host module)
{ inputs, pkgs, ... }:
{
  imports = [ inputs.issues-manager.nixosModules.default ];

  services.issues = {
    enable = true;
    package = inputs.issues-manager.packages.${pkgs.system}.default;
    bindAddress = "192.0.2.10";          # a private interface; default 0.0.0.0
    port = 8787;
    tokenFile = "/run/secrets/issues-token";  # optional
    openFirewall = true;
  };
}
```

Options under `services.issues`:

| Option         | Type           | Default                    | Notes                                                     |
|----------------|----------------|----------------------------|-----------------------------------------------------------|
| `enable`       | bool           | `false`                    |                                                           |
| `package`      | package        | built from this repo       | Set to the flake's package for a pinned build             |
| `port`         | port           | `8787`                     |                                                           |
| `bindAddress`  | str            | `"0.0.0.0"`                | Bind to a private or VPN address to limit reachability    |
| `dataDir`      | path           | `/var/lib/issues`          | With the default, systemd `StateDirectory` provisions it; otherwise create it yourself, writable by the service |
| `tokenFile`    | null or path   | `null`                     | Static bearer token; `null` means no authentication       |
| `openFirewall` | bool           | `false`                    | Open `port` in the NixOS firewall                         |

The service runs as a `DynamicUser` with the usual systemd hardening
(`ProtectSystem=strict`, `ProtectHome`, `PrivateTmp`, `NoNewPrivileges`, ...)
and only `dataDir` writable.

## Security notes

- There is no user management, no TLS and, by default, no authentication. Run
  it on a network you trust: a VPN or overlay network, a LAN, or bound to
  `127.0.0.1` behind a reverse proxy that handles TLS and auth.
- The optional bearer token is a single shared secret sent in clear text over
  HTTP. Use it together with network isolation or TLS, not instead of them.
- Anyone with API access can create, modify and archive any issue in any
  project. There is no delete endpoint; archived files stay on disk.
- Back up the data directory like any other set of files. Nothing else holds
  state.

## Development

```sh
go test ./...
go run ./cmd/issues serve -data /tmp/issues-data -addr 127.0.0.1
ISSUES_URL=http://127.0.0.1:8787 go run ./cmd/issues ls
```

Layout:

```
cmd/issues/        single binary: server + CLI
internal/store/    markdown store: CRUD, numbering, archive, frontmatter
internal/api/      HTTP handlers
internal/client/   HTTP client used by the CLI
nix/package.nix    buildGoModule package
nix/module.nix     NixOS module (services.issues)
flake.nix          package + nixosModule + devShell
```

## License

MIT. See [LICENSE](LICENSE).
