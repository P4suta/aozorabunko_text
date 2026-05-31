# aozorabunko_text

[青空文庫](https://www.aozora.gr.jp) で公開されているテキストを、**人間が読めるパス**と **UTF-8** で再構成したミラーです。

```
作品/
├─ 夏目漱石/
│  ├─ 吾輩は猫である.txt
│  └─ こころ.txt
├─ 芥川龍之介/
│  ├─ 羅生門.txt
│  └─ 蜘蛛の糸.txt
└─ …
```

元の青空文庫（および従来の配布形式）は `cards/000005/files/53194_ruby_44732/…` のような
ID ベースのパスで、ファイルは Shift_JIS のため GitHub 上では文字化けします。
このリポジトリでは青空文庫のメタデータと突き合わせて `作品/<著者名>/<作品名>.txt` に並べ替え、
中身を **UTF-8 に変換**しているので、ブラウザでもそのまま読めます。

- `作品/` … 著者名・作品名でマッチできた本編テキスト（UTF-8）
- `_unmatched/` … メタデータに対応が見つからなかったもの（ID パスのまま）
- `_decode_errors/` … Shift_JIS のデコードに問題があったもの（隔離）

## 動作のしくみ

[aozorabunko/aozorabunko](https://github.com/aozorabunko/aozorabunko) の中身と
青空文庫のメタデータ（`list_person_all_extended_utf8.csv`）を取得し、各作品の zip から
本編テキストを取り出して UTF-8 に変換し、著者名・作品名のパスに保存しています。

抽出処理は Go (`cmd/aozora`) で実装しており、外部依存はテキスト変換用の
[`golang.org/x/text`](https://pkg.go.dev/golang.org/x/text) のみです。
GitHub Actions (`.github/workflows/update_daily.yml`) 上で1日1回バッチ動作し、
手動実行したい場合は Actions の "Update works" ワークフローを `workflow_dispatch` で起動できます。

ローカルで実行する場合:

```console
$ git clone --depth 1 https://github.com/aozorabunko/aozorabunko.git
$ curl -sSL -o list.zip https://www.aozora.gr.jp/index_pages/list_person_all_extended_utf8.zip && unzip -o list.zip
$ go run ./cmd/aozora -src aozorabunko -meta list_person_all_extended_utf8.csv -dest .
```

既に存在する出力ファイルはスキップされるため、再実行は差分のみになります。

## 権利関係

`作品/` 内のファイルは、[「青空文庫収録ファイルの取り扱い規準」](https://www.aozora.gr.jp/guide/kijyunn.html)
の元でご利用ください。本リポジトリは、これらの著作物を文字コード変換・再配置した非公式ミラーです。
著作権保護期間が終了しておらず、クリエイティブ・コモンズ・ライセンス等による許諾の元で
再配布されているファイルも含まれています。ご注意ください。
