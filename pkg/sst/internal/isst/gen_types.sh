#!/bin/bash -e

set -o pipefail

tmpfile="_types_out.go"
trap "rm -f $tmpfile" EXIT

generate() {
    local target="$1"
    shift
    local copts=$@

    echo "Generating $target..."

    go tool cgo -godefs -- $copts _"$target" | gofmt > "$tmpfile"
    mv "$tmpfile" "$target"
}

KERNEL_SRC_DIR="${KERNEL_SRC_DIR:-/usr/src/linux}"

echo "INFO: using kernel sources at $KERNEL_SRC_DIR"

# Generate types from Linux kernel (public) headers
generate types_amd64.go -I"$KERNEL_SRC_DIR/include/uapi" "-I$KERNEL_SRC_DIR/include"
generate types_msr_amd64.go -I"$KERNEL_SRC_DIR/include/uapi" "-I$KERNEL_SRC_DIR/include"

# Generate constants from Linux kernel private headers (isst tool sources)
generate types_priv.go "-I$KERNEL_SRC_DIR" "-I$KERNEL_SRC_DIR/include" "-I$KERNEL_SRC_DIR/arch/x86/include/generated/"
