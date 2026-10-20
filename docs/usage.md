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

Use an existing local checkout of the original Aozora Bunko archive tree.
Its `cards/` directory must contain the original text ZIP files.
Expanded text files from other mirrors don't satisfy this input contract.
The original GitHub source now returns HTTP 404.
This repository no longer downloads it on a schedule or through Actions.

Run these commands from the repository root after placing the archive tree at `../aozorabunko`.

```sh
curl --fail --location -o list.zip https://www.aozora.gr.jp/index_pages/list_person_all_extended_utf8.zip
unzip -o list.zip
mise x -- go run ./cmd/aozora -src ../aozorabunko -meta list_person_all_extended_utf8.csv -dest . -workers 2
```

The converter reads the upstream archives and metadata, then converts the texts from Shift_JIS to UTF-8.
It skips existing output files.

## Source recovery

Automatic updates require a verified source for the original ZIP archives.
[Issue 9](https://github.com/P4suta/aozorabunko_text/issues/9) tracks that work.
The existing texts form a stored snapshot rather than a live mirror.
Review each work's source and original usage terms before redistributing it.
