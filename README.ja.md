# issues-manager

markdown ファイルを正本とする、小さなセルフホスト型の Issue 管理ツールです。
ディレクトリの上に薄い HTTP API と CLI（`issues`）を載せています。
DB もクラウド認証もなく、信頼できるプライベートネットワーク（自宅ラボ・VPN・LAN）での
利用を想定しています。

詳細（API 一覧・NixOS module のオプション・セキュリティ上の注意）は
英語版 [README.md](README.md) を参照してください。ここでは要点だけまとめます。

## ビルド

```sh
go build -o issues ./cmd/issues
nix build github:Socrates10101/Issues-manager
```

## サーバー

```sh
issues serve -data /var/lib/issues -addr 127.0.0.1 -port 8787 [-token-file FILE]
```

環境変数 `ISSUES_DATA` / `ISSUES_ADDR` / `ISSUES_PORT` / `ISSUES_TOKEN_FILE` でも指定できます。

## ファイル形式

```
<data>/<project>/<NNN>-<slug>.md            アクティブ
<data>/archive/<project>/<NNN>-<slug>.md    アーカイブ済み
```

```markdown
---
project: infra
number: 8
title: タイトル
status: 新規
created: 2026-06-28
updated: 2026-06-28
---

本文（markdown）
```

- status は 5 段階固定: `新規 / 調査中 / 対応中 / 修正完了・検証待ち / 解決済`
- 採番はプロジェクト内の連番。欠番は再利用しない
- archive できるのは `解決済` のみ（それ以外はサーバーが 409 を返す）

## CLI

`ISSUES_URL`（既定 `http://127.0.0.1:8787`）と任意の `ISSUES_TOKEN` を読みます。

```sh
issues ls [-p PROJECT] [-s STATUS] [--archived] [--all]
issues show <project>:<NNN>
issues new "TITLE" -p PROJECT
issues status <project>:<NNN> "<STATUS>"
issues archive <project>:<NNN>
```

## NixOS

```nix
imports = [ inputs.issues-manager.nixosModules.default ];
services.issues = {
  enable = true;
  package = inputs.issues-manager.packages.${pkgs.system}.default;
  bindAddress = "192.0.2.10";
  openFirewall = true;
};
```

## ライセンス

MIT
