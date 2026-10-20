# Usage

## Directory layout

- `作品/` contains texts matched to an author and title.
- `_unmatched/` contains texts without a metadata match.
- `_decode_errors/` quarantines decoding failures.

## Check the converter

Review the tool configuration before trusting it.

```sh
mise trust .mise.toml
mise install go
mise x -- go mod verify
mise x -- go vet ./...
mise x -- go test ./...
mise x -- go tool govulncheck ./...
```

The converter has no behavior tests yet.
The test command checks compilation until tests exist.
The vulnerability check uses the official tool pinned through `go.mod`.

## Rebuild the mirror

Run these commands from the repository root.

```sh
git clone --depth 1 --filter=blob:none --sparse https://github.com/aozorabunko/aozorabunko.git aozorabunko
git -C aozorabunko sparse-checkout set --no-cone '/cards/**/*.zip'
curl --fail --location -o list.zip https://www.aozora.gr.jp/index_pages/list_person_all_extended_utf8.zip
unzip -o list.zip
mise x -- go run ./cmd/aozora -src aozorabunko -meta list_person_all_extended_utf8.csv -dest . -workers 2
```

The converter reads the upstream archives and metadata, then converts the texts from Shift_JIS to UTF-8.
It skips existing output files.

## Refresh workflow

The weekly job runs on Monday.
Run **Update works** manually for an earlier refresh.
Its `dry-run` input generates the corpus without opening a proposal.

Changed output produces a signed draft on `automation/corpus-refresh`.
The workflow checks the commit signature and dispatches the required converter checks for that branch.
Review the data and its original usage terms before merging the draft.
