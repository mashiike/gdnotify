# ADR-001: EventBridge通知時のオプショナルS3コピー機能

**ステータス**: 提案中

## コンテキスト

gdnotifyは現在、Google Driveの変更をEventBridge経由で通知している。通知には変更されたファイルのメタデータ（ファイルID、名前、変更者など）が含まれるが、ファイルの実体は含まれない。

典型的なユースケースでは、EventBridge通知を受け取った後続処理（Lambda等）がGoogle Drive APIを呼び出してファイルをS3にダウンロードしている。

```
現状のフロー:
Google Drive → gdnotify → EventBridge → Lambda → Google Drive API → S3
```

この構成には以下の課題がある：

1. **後続処理の複雑化**: 各後続処理がGoogle Drive APIの認証・ファイル取得ロジックを実装する必要がある
2. **認証情報の分散**: gdnotifyと後続処理の両方がGoogle Driveの認証情報を持つ必要がある
3. **重複実装**: 複数の後続処理が同じファイル取得ロジックを実装することがある

## 提案

EventBridge通知の**オプション機能**として、gdnotifyがファイルをS3にコピーし、そのS3 URIを通知に含める機能を追加する。

```
提案するフロー:
Google Drive → gdnotify → S3 → EventBridge (S3 URI含む)
                    ↓
                  Lambda (S3から直接取得可能)
```

### 通知ペイロードの拡張案

```json
{
  "subject": "File gdnotify (XXXXXXXXXX) changed by hoge...",
  "entity": { ... },
  "actor": { ... },
  "change": { ... },
  "s3Copy": {
    "s3Uri": "s3://my-bucket/files/2024/file-id/filename.xlsx",
    "contentType": "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    "size": 12345,
    "copiedAt": "2024-01-07T12:34:56Z"
  }
}
```

## 決定事項

### 設定方式: YAML設定ファイル + CEL（Common Expression Language）

設定方法の検討の結果、**YAML設定ファイルにCEL式を埋め込む方式**を採用する。

#### 採用理由

1. **柔軟性**: 複雑な条件（MIMEタイプ、ファイルサイズ、フォルダ、ユーザーなど）をCEL式で表現可能
2. **デファクト**: Kubernetes ValidatingAdmissionPolicy、Google Cloud IAM Conditions等、CELを使う設定ファイルはYAMLが標準
3. **動的パス生成**: `object_key` をCEL式で柔軟に生成できる
4. **バージョン管理**: 設定ファイルをGit管理可能

#### 設定ファイル形式

```yaml
# s3-copy.yaml

# デフォルト設定（rulesで省略時に使用）
bucket_name: my-bucket
object_key: |
  "files/" + string(change.time.getFullYear()) + "/" + file.id + "/" + file.name

rules:
  # PDFはそのままダウンロード（bucket_name, object_key省略 → トップレベルを使用）
  - when: "file.mimeType == 'application/pdf'"

  # Google Workspace系は別バケット・PDFエクスポート
  - when: "file.mimeType.startsWith('application/vnd.google-apps')"
    export: pdf
    bucket_name: workspace-exports-bucket
    object_key: |
      "exports/" + file.id + "/" + file.name + ".pdf"

  # 特定フォルダは別パス構造
  - when: "file.parents.exists(p, p == '1234567890abcdef')"
    object_key: |
      "special-folder/" + file.name

  # 100MB超はスキップ
  - when: "file.size > 100 * 1024 * 1024"
    skip: true

  # 画像はスキップ
  - when: "file.mimeType.startsWith('image/')"
    skip: true
```

#### CLIからの利用

```bash
gdnotify serve --s3-copy-config=./s3-copy.yaml
```

#### CEL式で利用可能な変数

| 変数 | 型 | 説明 |
|------|-----|------|
| `file.id` | string | ファイルID |
| `file.name` | string | ファイル名 |
| `file.mimeType` | string | MIMEタイプ |
| `file.size` | int | ファイルサイズ（バイト） |
| `file.modifiedTime` | timestamp | 最終更新日時 |
| `file.parents` | list(string) | 親フォルダIDのリスト |
| `actor.emailAddress` | string | 変更者のメールアドレス |
| `actor.displayName` | string | 変更者の表示名 |
| `change.time` | timestamp | 変更検知日時 |
| `change.changeType` | string | 変更種別 |

#### ルール評価の仕組み

1. rulesは上から順に評価
2. 最初にマッチした（`when`がtrueになった）ルールを適用
3. `skip: true` のルールにマッチした場合、S3へコピーしない
4. どのルールにもマッチしない場合、S3へコピーしない（明示的なルール必須）

## 検討事項

### 1. Google Workspace ファイルの扱い

Google Docs/Sheets/Slides等のGoogle Workspace形式ファイルは直接ダウンロードできず、エクスポートが必要。

対応するエクスポート形式:
- Google Docs → `docx`, `pdf`, `txt`, `html`
- Google Sheets → `xlsx`, `pdf`, `csv`
- Google Slides → `pptx`, `pdf`

### 2. エラーハンドリング

S3コピー失敗時の動作：

- 通知自体を送らない？
- 通知は送るが `s3Copy.error` フィールドを含める？
- リトライ戦略は？

### 3. パフォーマンス・コスト

- ファイルサイズ上限の設定
- 同時ダウンロード数の制限
- S3への転送コスト

### 4. セキュリティ

- S3バケットへのIAM権限
- ファイルの暗号化設定
- アクセスログ

## 未確定事項

- [x] 機能名称: `s3-copy` を採用
- [ ] Google Workspace ファイルのデフォルトエクスポート形式は何にするか？
- [ ] S3コピー失敗時に通知を送るか送らないか？
- [ ] ファイルサイズ上限のデフォルト値は？
- [ ] どのルールにもマッチしない場合の挙動（現案: S3へコピーしない）

## 参考

- [CEL - Common Expression Language](https://github.com/google/cel-spec)
- [cel-go - Go implementation](https://github.com/google/cel-go)
- [Google Drive API - Files: export](https://developers.google.com/drive/api/reference/rest/v3/files/export)
- [Google Drive API - Files: get](https://developers.google.com/drive/api/reference/rest/v3/files/get)
