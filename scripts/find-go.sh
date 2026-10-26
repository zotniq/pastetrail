#!/bin/sh

if command -v go >/dev/null 2>&1; then
    command -v go
    exit 0
fi

for candidate in /usr/local/go/bin/go /opt/homebrew/bin/go /usr/local/bin/go /opt/homebrew/opt/go/bin/go; do
    if [ -x "$candidate" ]; then
        printf '%s\n' "$candidate"
        exit 0
    fi
done

case "$(uname -s)" in
    Darwin) go_os=darwin ;;
    Linux) go_os=linux ;;
    *) exit 0 ;;
esac
case "$(uname -m)" in
    arm64|aarch64) go_arch=arm64 ;;
    x86_64|amd64) go_arch=amd64 ;;
    i?86) go_arch=386 ;;
    *) exit 0 ;;
esac

# Cached toolchains include the OS and architecture in their directory names.
# Keep the default cache as a fallback when a build uses an isolated GOMODCACHE.
{
    printf '%s\n' "${GOMODCACHE:-}"
    printf '%s\n' "${GOPATH:-$HOME/go}" | tr ':' '\n' | while IFS= read -r root; do
        [ -n "$root" ] && printf '%s/pkg/mod\n' "$root"
    done
    printf '%s/go/pkg/mod\n' "$HOME"
} | while IFS= read -r cache; do
    [ -n "$cache" ] || continue
    for candidate in "$cache"/golang.org/toolchain@v0.0.1-go*."$go_os-$go_arch"/bin/go; do
        [ -x "$candidate" ] || continue
        version=${candidate##*/toolchain@v0.0.1-go}
        version=${version%.$go_os-$go_arch/bin/go}
        case "$version" in
            ''|*[!0-9.]*) continue ;;
        esac
        printf '%s %s\n' "$version" "$candidate"
    done
done | sort -t . -k1,1n -k2,2n -k3,3n | tail -n 1 | cut -d ' ' -f 2-
