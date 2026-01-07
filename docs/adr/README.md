# Architectural Decision Records (ADR)

gdnotify プロジェクトの重要な設計判断を記録する。

## ADRとは？

なぜその設計を選んだのかを記録するドキュメント。将来の開発者（AIエージェント含む）が同じ議論を繰り返さないために使う。

## ディレクトリ構成

```
docs/adr/
├── README.md              # このファイル
├── templates/
│   ├── proposal.md        # 提案中（未実装）用
│   └── standard.md        # 適用済み用
├── 000-example.md         # 3桁の通し番号
└── ...
```

## ADRの書き方

1. 次の番号を確認: `ls docs/adr/*.md`
2. 新規作成: `docs/adr/001-your-decision.md`
3. `templates/` から適切なテンプレートを使う

## ルール

- 既存ADRは書き換えない。新規ADRを追加する
- 3桁の通し番号を使う（例: `001`, `002`, `001.1`）
- **Why** を書く。How はコードを見ればわかる

## 参考

- [Wantedly Engineering Handbook - ADR](https://docs.wantedly.dev/fields/dev-process/adr)
- [GitHub ADR Organization](https://adr.github.io/)
