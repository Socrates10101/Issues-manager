# issues

dotfiles から切り離した **Issue 管理の SSOT**。markdown ストレージの上に薄い HTTP CRUD
サービス + CLI を載せたもの。クラウド認証なし・ssh/LAN で疎通・git 非依存。

- **設計（正本）**: `dotfiles/docs/issues-system-design.md`。配置先・トラスト境界・durability・
  endpoint の確定版はそちら（本 README は実装/CLI/API のリファレンス）。
- **確定**: server は host 非依存 module、稼働は **gem の issues-vm**（トラスト域・tailnet・secret lane）。
  endpoint は `ISSUES_URL`(MagicDNS) を dotfiles に1回・全環境同一。データは dataDir + **GCS 版付き backup**（送り先は将来 4TB へ差替可）。
- データ SSOT = サービス稼働ホストの `dataDir`（永続ディスク上の markdown 群）。

## 構成

```
cmd/issues/         server + client + migrate を兼ねる単一バイナリ
internal/store/     markdown ストア（CRUD・採番・archive・frontmatter）
internal/api/       HTTP ハンドラ
internal/client/    CLI 用 HTTP クライアント
internal/migrate/   旧 dotfiles Issues/ の取り込み
nix/package.nix     buildGoModule
nix/module.nix      services.issues（ホスト非依存 NixOS module）
flake.nix           package + nixosModule を export
```

## on-disk フォーマット

```
<dataDir>/<project>/<NNN>-<slug>.md           active
<dataDir>/archive/<project>/<NNN>-<slug>.md    archived
```

```markdown
---
project: homelab
number: 8
title: WAF カスタムルール (Phase2-C)
status: 解決済
created: 2026-06-28
updated: 2026-06-28
---
（本文 markdown。ssh で cat しても読める）
```

- status は 5 段階: `新規 / 調査中 / 対応中 / 修正完了・検証待ち / 解決済`
- 採番はプロジェクト内連番・欠番は詰めない
- archive は status=`解決済` のみ許可（サーバーが 409 で安全弁）

## CLI

`$ISSUES_URL`（既定 `http://127.0.0.1:8787`）と任意の `$ISSUES_TOKEN` を見る。

```sh
issues ls [-p PROJECT] [-s STATUS] [--archived] [--all]
issues show <project>:<NNN>
issues new "TITLE" -p PROJECT
issues status <project>:<NNN> "<STATUS>"
issues archive <project>:<NNN>
```

## HTTP API

| メソッド/パス | 動作 |
|---|---|
| `GET /healthz` | 死活 |
| `GET /issues?project=&status=&archived=` | 一覧 |
| `GET /issues/{project}/{number}` | 詳細 |
| `POST /issues` `{project,title,body?}` | 作成（採番） |
| `PATCH /issues/{project}/{number}` `{status?,title?,body?}` | 更新 |
| `POST /issues/{project}/{number}/archive` | archive |
| `GET /projects` | プロジェクト一覧 |

任意の Bearer トークン認証（`-token-file` / `services.issues.tokenFile`）。

## ローカル実行

```sh
go test ./...
go run ./cmd/issues serve -data /tmp/idata -addr 127.0.0.1 -port 8787
go run ./cmd/issues migrate -from ~/.dotfiles/Issues -data /tmp/idata
ISSUES_URL=http://127.0.0.1:8787 go run ./cmd/issues ls
```

## デプロイ（NixOS / homelab）

homelab の `flake.nix` に input を足し、任意のホストで enable する:

```nix
# inputs
issues.url = "github:Socrates10101/issues";   # or path:

# 該当ホストの configuration
imports = [ inputs.issues.nixosModules.default ];
services.issues = {
  enable = true;
  package = inputs.issues.packages.${pkgs.system}.default;
  port = 8787;
  bindAddress = "0.0.0.0";   # LAN 用。Tailscale 限定なら tailscale0 IP
  openFirewall = true;
};
```

`services.issues` はホスト固有値を option 化してあるので、lab1 でも gem12 上の
microvm でも `enable` 一発で載る。

## dotfiles からの切替（cutover、ホスト決定後に実施）

1. ホストを決めて上記の通り `nixos-rebuild` でデプロイ
2. 稼働ホスト上で `issues migrate -from <dotfilesの Issues/> -data <dataDir>` を一度実行
3. `issues ls --all` で 46 active + 23 archive = 69 件を突合
4. dotfiles 側を切替:
   - `config/claude/skills/issues-manager/{SKILL.md,reference.md}` を本サービスの API/CLI クライアント記述へ改訂
   - `config/tsk/config.yaml` の `issues_root` を削除（`projects:` は tmx 用に残す）
   - 各マシンの dotfiles（`aliases/` 等）に `ISSUES_URL` を設定
5. 切替・検証完了後に dotfiles の `Issues/` を削除（**それまでは Issues/ が現行 SSOT**）
