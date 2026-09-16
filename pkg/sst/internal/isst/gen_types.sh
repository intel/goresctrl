#!/bin/bash -e

set -o pipefail

tmpfile="_types_out.go"
trap "rm -f $tmpfile" EXIT

# types.go compiles on every architecture so that importers of pkg/sst build
# everywhere, but its contents are the Linux/amd64 ABI. Refuse to generate it
# for anything else.
target_os="$(go env GOOS)"
target_arch="$(go env GOARCH)"
if [ "$target_os" != "linux" ] || [ "$target_arch" != "amd64" ]; then
    echo "ERROR: ISST types must be generated for the linux/amd64 ABI, not $target_os/$target_arch" >&2
    exit 1
fi

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
generate types.go -I"$KERNEL_SRC_DIR/include/uapi" "-I$KERNEL_SRC_DIR/include"
generate types_msr_amd64.go -I"$KERNEL_SRC_DIR/include/uapi" "-I$KERNEL_SRC_DIR/include"

# Generate constants from Linux kernel private headers (isst tool sources)
generate types_priv.go "-I$KERNEL_SRC_DIR" "-I$KERNEL_SRC_DIR/include" "-I$KERNEL_SRC_DIR/arch/x86/include/generated/"
