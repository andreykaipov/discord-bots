#!/bin/sh
set -eu

root=$(git rev-parse --show-toplevel)
repo=$(basename "$root")
log() { printf '\033[1;33m%s\033[0m\n' "$*" >&2; }
get() { op read "op://github/$repo/$1"; }

main() {
        log "Setting up $repo"
        : "${OP_SERVICE_ACCOUNT_TOKEN?needs to be set for op CLI}"
        op user get --me

        bots=$(find "$root/go" -maxdepth 2 -name go.mod -printf '%P\n' | xargs -n1 dirname)
        for bot in $bots; do
                get_secrets "$bot"
        done
}

get_secrets() {
        bot=${1?}
        appid=$(get "$bot/app-id")
        pubkey=$(get "$bot/public-key")
        token=$(get "$bot/token")

        cat >"$root/go/$bot/.env" <<EOF
# vi: ft=sh
export DISCORD_APPLICATION_ID=$appid
export DISCORD_PUBLIC_KEY=$pubkey
export DISCORD_TOKEN=$token
EOF
}

main "$@"
