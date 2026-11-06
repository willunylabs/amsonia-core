#!/usr/bin/env bash
set -euo pipefail

release_tag="$1"
output="$2"
[[ "$release_tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+-preview\.[0-9]+$ ]]
mkdir -p "$output"
cd "$output"
version="${release_tag#v}"
bundle="amsonia-core-$version-linux-amd64"
base="https://github.com/willunylabs/amsonia-core/releases/download/$release_tag"
for file in SHA256SUMS "$bundle.tar.gz"; do
  curl --fail --silent --show-error --location --retry 3 --max-time 180 \
    --proto '=https' --proto-redir '=https' --output "$file" "$base/$file"
done
sha256sum --check SHA256SUMS
tar -xzf "$bundle.tar.gz"
test "$(cat "$bundle/VERSION")" = "$version"
revision="$(git ls-remote https://github.com/willunylabs/amsonia-core.git \
  "refs/tags/$release_tag" "refs/tags/$release_tag^{}" | tail -1 | cut -f1)"
test -n "$revision"
test "$(cat "$bundle/REVISION")" = "$revision"
echo "Verified public download: $release_tag at $revision"
for image in api web postgres; do
  if docker image inspect "amsonia-core-$image:$version" >/dev/null 2>&1; then
    echo "Unexpected preloaded release image: $image" >&2
    exit 1
  fi
done
