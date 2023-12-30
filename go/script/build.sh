#!/bin/sh

set -eu

root=$(git rev-parse --show-toplevel)

: "${tag:?}"
: "${module:?}"
: "${dir:?}"

docker build -t "$tag" --build-arg module="$module" --build-arg dir="$dir" "$root/go"
docker push "$tag"
