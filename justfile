test:
    go test ./...

# Build every examples/ package that carries a //go:build jsonschema
# registration file, catching compile errors that `go test ./...` can't see
# (those files are excluded from the default build).
build-tagged:
    #!/usr/bin/env bash
    set -euo pipefail
    pkgs=$(grep -rl '^//go:build jsonschema$' --include='*.go' examples | xargs -n1 dirname | sort -u | sed 's#^#./#')
    go build -tags jsonschema $pkgs

watch focus:
    # no ginkgo; use `go test` with -run for focus
    go test ./... -run '{{focus}}'

testregistry:
    cd internal/typeregistry && go test ./...

# The goimports step formats only this module's own sources. ephemeral/ holds
# tracked session artifacts with their own go.mod files (so ./... already skips
# them, but a bare find does not), and node_modules is never ours to rewrite.
lint:
    go mod tidy
    modernize -fix ./...
    go vet ./...
    go run honnef.co/go/tools/cmd/staticcheck@v0.8.2 ./...
    govulncheck ./...
    golangci-lint run ./...
    find . \( -path ./.git -o -path ./ephemeral -o -name node_modules \) -prune -o -name '*.go' -exec goimports -w {} +

update-deps:
    #!/usr/bin/env sh
    for f in $(fd go.mod); do
        pushd $(dirname $f)
        go mod tidy
        popd
    done

upgrade-module mod-path:
    #!/usr/bin/env sh
    for f in $(fd go.mod); do
        pushd $(dirname $f)
        go get -u $mod
        go mod tidy
        popd
    done
