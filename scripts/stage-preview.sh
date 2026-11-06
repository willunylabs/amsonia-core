#!/usr/bin/env bash
set -euo pipefail
: "${GITHUB_REPOSITORY:?missing repository}"
: "${PREVIEW_RUN_ID:?missing preview run ID}"
[[ "$PREVIEW_RUN_ID" =~ ^[1-9][0-9]*$ ]]
repo="$GITHUB_REPOSITORY"
sha="$(gh api "repos/$repo/actions/runs/$PREVIEW_RUN_ID" | jq -er --arg repo "$repo" '
    select(.head_repository.full_name == $repo and .head_branch == "main" and
        .path == ".github/workflows/preview.yml" and .event == "workflow_dispatch" and
        .status == "completed" and .conclusion == "success") | .head_sha')"
[[ "$sha" =~ ^[a-f0-9]{40}$ ]]

stage="$(mktemp -d)"
trap 'rm -rf -- "$stage"' EXIT
gh run download "$PREVIEW_RUN_ID" --repo "$repo" --name amsonia-preview-linux-amd64 --dir "$stage"
cd "$stage"
shopt -s nullglob
archives=(amsonia-core-*-linux-amd64.tar.gz)
[[ "${#archives[@]}" == 1 ]]
archive="${archives[0]}"
bundle="${archive%.tar.gz}"
version="$(tar -xOzf "$archive" "$bundle/VERSION")"
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+-preview\.[0-9]+$ ]]
[[ "$bundle" == "amsonia-core-$version-linux-amd64" ]]
[[ "$(tar -xOzf "$archive" "$bundle/REVISION")" == "$sha" ]]
cmp <(sha256sum "$archive") SHA256SUMS
tag="v$version"

if ! gh release view "$tag" --repo "$repo" --json isDraft,isPrerelease,targetCommitish,assets > draft.json; then
    tar -xOzf "$archive" "$bundle/CHANGELOG.md" > notes.md
    gh release create "$tag" --repo "$repo" --draft --prerelease --latest=false \
        --target "$sha" --title "Amsonia Core $version" --notes-file notes.md
    gh release view "$tag" --repo "$repo" --json isDraft,isPrerelease,targetCommitish,assets > draft.json
fi
jq -e --arg sha "$sha" '.isDraft and .isPrerelease and .targetCommitish == $sha' draft.json >/dev/null

# Never overwrite an existing attachment. A retry accepts only exact bytes.
missing=()
for file in "$archive" SHA256SUMS; do
    digest="sha256:$(sha256sum "$file" | cut -d ' ' -f 1)"
    existing="$(jq -r --arg name "$file" '.assets[] | select(.name == $name) | .digest // "unavailable"' draft.json)"
    if [[ -z "$existing" ]]; then
        missing+=("$file")
    elif [[ "$existing" != "$digest" ]]; then
        echo "Existing attachment does not match: $file" >&2
        exit 1
    fi
done
if [[ "${#missing[@]}" -gt 0 ]]; then
    gh release upload "$tag" "${missing[@]}" --repo "$repo"
fi
gh release view "$tag" --repo "$repo" --json url,isDraft,targetCommitish,assets
