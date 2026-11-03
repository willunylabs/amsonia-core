#!/usr/bin/env bash
set -euo pipefail
if [[ $# -ne 1 ]]; then
    echo 'usage: scripts/build-preview.sh <output-directory>' >&2
    exit 2
fi
mkdir -p "$1"
out="$(cd -- "$1" && pwd)"
cd -- "$(dirname -- "$0")/.."
version="$(node -p "require('./web/package.json').version")"
api_version="$(awk '/^  version: / { print $2; exit }' openapi/openapi.yaml)"
lock_version="$(node -p "const p=require('./web/package-lock.json'); p.version === p.packages[''].version ? p.version : ''")"
if [[ ! "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-preview\.[0-9]+$ || "$version" != "$api_version" || "$version" != "$lock_version" ]]; then
    echo 'Preview package, lockfile and OpenAPI versions must match.' >&2
    exit 1
fi
if [[ "${GITHUB_REF_TYPE:-}" == tag && "${GITHUB_REF_NAME:-}" != "v$version" ]]; then
    echo "Tag must match v$version." >&2
    exit 1
fi
name="amsonia-core-${version}-linux-amd64"
stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
bundle="$stage/$name"
mkdir -p "$bundle/ops" "$bundle/docs"
cp compose.yaml LICENSE CHANGELOG.md "$bundle/"
cp ops/{compose_configure_roles,configure_database_roles}.sql "$bundle/ops/"
cp docs/{self-hosting,getting-started}.md "$bundle/docs/"
cp deploy/preview/{README.md,compose.preview.yaml,init-env,amsonia-compose} "$bundle/"
printf '%s\n' "$version" > "$bundle/VERSION"
git rev-parse HEAD > "$bundle/REVISION"
for component in api web; do
    docker build --platform linux/amd64 -f "Dockerfile.$component" \
        --label "org.opencontainers.image.revision=$(cat "$bundle/REVISION")" \
        --label "org.opencontainers.image.version=$version" \
        -t "amsonia-core-$component:$version" .
done
docker pull --platform linux/amd64 postgres:16-alpine
docker tag postgres:16-alpine "amsonia-core-postgres:$version"
docker image save -o "$bundle/images.tar" \
    "amsonia-core-api:$version" "amsonia-core-web:$version" "amsonia-core-postgres:$version"
tar -C "$stage" -czf "$out/$name.tar.gz" "$name"
(cd "$out" && shasum -a 256 "$name.tar.gz" > SHA256SUMS)
