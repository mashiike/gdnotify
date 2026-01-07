# AGENTS.md – gdnotify 開発ガイド

## このファイルについて

**目的**: コードへの索引と、経緯の追い方のガイド

**すべてのコーディングエージェント（Claude、Gemini、Codex等）へ**:
- コードを読めばわかることはここに書かない
- 設計判断の経緯は [docs/adr/](docs/adr/) に記録する
- このファイルは「どこを見ればわかるか」の索引として使う

---

## 経緯の追い方

### ADR（Architecture Decision Records）

アーキテクチャレベルの設計判断は [docs/adr/](docs/adr/) に記録している。

**ADRを見るべきとき**:
- なぜこのアーキテクチャなのか知りたいとき
- 技術選定の背景を知りたいとき
- トレードオフの判断理由を知りたいとき

**ADRの書き方**: [docs/adr/README.md](docs/adr/README.md) を参照

### PR・コミット

細かい変更の経緯はPRやコミットメッセージで追跡する：
- `git log --oneline` で変更履歴を確認
- `git blame <file>` で特定行の変更者・コミットを確認
- PRのdescriptionやコメントで議論の経緯を確認

---

## コードの読み方

### 最初に見るべき場所

| 知りたいこと | 見るべき場所 |
|-------------|-------------|
| CLIオプション一覧 | `cli.go` の構造体タグ |
| HTTPエンドポイント | `handler.go` の `setupRoute` |
| ビジネスロジック | `app.go` の各メソッド |

### インターフェースから読む

このプロジェクトは以下のインターフェースで抽象化されている：

| インターフェース | 定義場所 | 実装 |
|-----------------|---------|------|
| `Storage` | `storage.go` | `DynamoDBStorage`, `FileStorage` |
| `Notification` | `notification.go` | `EventBridgeNotification`, `FileNotification` |

**新しい実装を追加するとき**: インターフェースを実装し、`New*` 関数の switch 文に追加する。

### 外部APIとの連携を理解する

Google Drive Push Notifications の仕組みを知らないと `app.go` の意図がわからない：

1. `changes.watch` で通知チャネルを登録（有効期限あり）
2. Google から Webhook で変更通知が来る
3. `changes.list` で実際の変更内容を取得
4. PageToken を更新して次回に備える

詳細は [Google Drive API ドキュメント](https://developers.google.com/drive/api/guides/push) を参照。

---

## コーディング規約

### コード修正時の原則

- **既存パターンを先に観察する**: 新しいコードを書く前に、類似の実装がどうなっているか確認する
- **ファイルの存在理由を理解する**: 複数ファイルに分かれているなら、それぞれの責務の違いを理解してから修正する

### コメント

- エクスポートされるシンボルには godoc コメントを書く
- コードを読めばわかることはコメントしない

---

## 注意事項

- テスト実行には DynamoDB Local が必要（`docker compose up -d dynamodb-local`）
