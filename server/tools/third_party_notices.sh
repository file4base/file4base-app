#!/bin/sh
# Writes the license and notice files of every Go module linked into a
# File4Base server binary (plus the Go toolchain, whose runtime and standard
# library are compiled in) to a single THIRD_PARTY_NOTICES file.
#
#   tools/third_party_notices.sh <server-binary> <output-file>
#
# The module list comes from the binary itself (`go version -m`), so it always
# matches what is shipped. Fails when a linked module has no license file.
set -eu

bin="$1"
out="$2"
modcache="$(go env GOMODCACHE)"
goroot="$(go env GOROOT)"

# Module cache paths escape upper-case letters as "!" + lower-case.
escape() { printf '%s' "$1" | sed 's/[A-Z]/!&/g' | tr 'A-Z' 'a-z'; }

section() {
    printf '\n================================================================================\n'
    printf '%s\n' "$1"
    printf '================================================================================\n'
}

{
    printf 'Third-party software included in the File4Base server (%s)\n' "$(basename "$bin")"
    printf 'File4Base itself is licensed under GPL-3.0-only.\n'

    section "Go programming language $(go env GOVERSION) (runtime and standard library)"
    # Packaged toolchains (e.g. Homebrew) keep the license next to GOROOT.
    golicense=""
    for d in "$goroot" "$goroot/.."; do
        [ -f "$d/LICENSE" ] && { golicense="$d"; break; }
    done
    if [ -z "$golicense" ]; then
        echo "third_party_notices: Go LICENSE not found near $goroot" >&2
        exit 1
    fi
    cat "$golicense/LICENSE"
    if [ -f "$golicense/PATENTS" ]; then printf '\n'; cat "$golicense/PATENTS"; fi

    go version -m "$bin" | awk '$1 == "dep" { print $2, $3 }' | while read -r path version; do
        dir="$modcache/$(escape "$path")@$version"
        files="$(find "$dir" -maxdepth 1 -type f \( -iname 'LICEN[CS]E*' -o -iname 'COPYING*' -o -iname 'NOTICE*' -o -iname 'PATENTS*' \) | sort)"
        if [ -z "$files" ]; then
            echo "third_party_notices: no license file for $path $version in $dir" >&2
            exit 1
        fi
        section "$path $version"
        printf 'Source code: https://proxy.golang.org/%s/@v/%s.zip\n\n' "$(escape "$path")" "$version"
        for f in $files; do
            printf -- '--- %s ---\n' "$(basename "$f")"
            cat "$f"
            printf '\n'
        done
    done
} > "$out"
