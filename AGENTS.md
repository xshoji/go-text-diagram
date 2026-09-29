# AGENTS.md

このリポジトリで変更を行うエージェント向けの作業指針です。

## プロジェクト概要

- Go 1.23以降で動作する単一CLIです。実行入口は`cmd/diagram`です。
- PlantUMLの構造図サブセットまたはDOTを読み込み、Unicode／ASCII図、DOT、GraphMLを出力します。
- PlantUML完全互換や、旧Graph-Easy風DSLとの互換性はありません。対応範囲は`README.md`とパーサーテストを基準にしてください。

## パッケージの責務

- `internal/plantuml`: PlantUMLの字句解析、構文解析、意味モデルへの変換
- `internal/exchange`: DOT入力とDOT／GraphML出力
- `internal/diagram`: 入力から得た不変の意味モデルと、その検証
- `internal/layout`: rank、座標、グループ境界、ダミーノードの計算
- `internal/route`: 直交経路、端点、A*、ラベル配置、経路品質の計算
- `internal/solution`: 配置・経路をまとめた不変snapshotと、その検証・品質比較
- `internal/solve`: 配置候補の生成、圧縮、transaction、最良候補の選択
- `internal/canvas`、`internal/render`、`internal/textwidth`: 端末セルへの描画と表示幅処理

CLIの処理順は次のとおりです。

```text
PlantUML ─→ plantuml ─┐                   ┌→ exchange → DOT / GraphML
                      ├→ diagram.Problem ─┤
DOT ─────→ exchange ──┘                   └→ solve（layout → route → solution）→ render
```

責務をまたぐ一時的な型変換やwrapperを追加する前に、既存の所有パッケージで直接変更できないか確認してください。`diagram.Problem`と`solution.Snapshot`の不変性、および候補の検証に失敗しても有効なbaselineを失わないtransaction境界を維持してください。

## 実装規約

- 標準ライブラリと既存パッケージを優先し、必要性のない依存関係や抽象化を追加しないでください。
- Goコードを変更したら`gofmt`を適用してください。
- 出力は決定的である必要があります。mapの反復順に依存する配置、経路、シリアライズを追加しないでください。
- 端末上の幅はbyte数やrune数ではなく`internal/textwidth`の表示幅で扱ってください。
- 入力エラーには、取得できる場合は形式、行、列を含めてください。未対応構文を黙って無視しないでください。
- レイアウトや経路探索には既存の資源上限があります。入力サイズに比例せず巨大なcanvasや探索空間を確保する変更は避けてください。

## テスト

変更したパッケージのテストを先に実行し、完了前に次を確認してください。

```sh
go test ./...
go vet ./...
```

CLI全体の描画、決定性、品質契約は次のE2Eテストで確認できます。

```sh
go test ./cmd/diagram -run '^TestE2E$' -count=1
```

表示結果を意図的に変更した場合は、E2Eの構造・品質指標を確認してから描画snapshotを更新してください。ハッシュをテスト通過だけの目的で変更してはいけません。snapshotの契約と更新履歴は`cmd/diagram/testdata/snapshots/README.md`にあります。

不具合修正では、再現入力を最も近いパッケージのテストへ追加してください。CLIを通さないと再現できない問題は`cmd/diagram/main_test.go`、実在規模の入力が必要な問題は`cmd/diagram/testdata/regression`へ追加します。
