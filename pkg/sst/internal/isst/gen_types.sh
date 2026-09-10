#!/bin/bash -e

set -o pipefail

tmpfile="_types_out.go"
trap "rm -f $tmpfile" EXIT

target_os="$(go env GOOS)"
target_arch="$(go env GOARCH)"
if [ "$target_os" != "linux" ] || [ "$target_arch" != "amd64" ]; then
    echo "ERROR: ISST types must be generated for the linux/amd64 ABI, not $target_os/$target_arch" >&2
    exit 1
fi

generate() {
    local source="$1"
    local target="$2"
    shift 2
    local copts=$@

    echo "Generating $target..."

    go tool cgo -godefs -- $copts "$source" | gofmt > "$tmpfile"
    mv "$tmpfile" "$target"
}

generate_isst_types() {
    local source="$1"
    shift
    local copts=$@

    echo "Generating types_amd64.go and types_nonamd64.go..."

    go tool cgo -godefs -- $copts "$source" | gofmt > "$tmpfile"
    cp "$tmpfile" types_amd64.go
    {
        echo "//go:build !amd64"
        echo
        cat "$tmpfile"
    } > types_nonamd64.go
}

KERNEL_SRC_DIR="${KERNEL_SRC_DIR:-/usr/src/linux}"

echo "INFO: using kernel sources at $KERNEL_SRC_DIR"

# Generate types from Linux kernel (public) headers
generate_isst_types _types_amd64.go -I"$KERNEL_SRC_DIR/include/uapi" "-I$KERNEL_SRC_DIR/include"
generate _types_msr_amd64.go types_msr_amd64.go -I"$KERNEL_SRC_DIR/include/uapi" "-I$KERNEL_SRC_DIR/include"

# Generate constants from Linux kernel private headers (isst tool sources)
generate _types_priv.go types_priv.go "-I$KERNEL_SRC_DIR" "-I$KERNEL_SRC_DIR/include" "-I$KERNEL_SRC_DIR/arch/x86/include/generated/"
