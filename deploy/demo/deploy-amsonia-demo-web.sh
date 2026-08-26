#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "deploy-amsonia-demo-web must run as root" >&2
  exit 1
fi

if [[ "$#" -ne 3 ]]; then
  echo "usage: deploy-amsonia-demo-web S3_ARTIFACT_URI RELEASE_SHA ARTIFACT_SHA256" >&2
  exit 2
fi

artifact_uri="$1"
release_sha="$2"
artifact_sha256="$3"

artifact_prefix="s3://h1-recon-results-336090301244-us-east-1/deployments/amsonia/demo-web/"
demo_root="/opt/amsonia-core-demo-web"
releases_root="${demo_root}/releases"
current_link="${demo_root}/current"
service_name="amsonia-core-web.service"

if [[ ! "${artifact_uri}" =~ ^${artifact_prefix}[0-9a-f]{40}/amsonia-demo-web-[0-9a-f]{40}\.tar\.gz$ ]]; then
  echo "artifact URI is outside the approved Amsonia Demo prefix" >&2
  exit 2
fi
if [[ ! "${release_sha}" =~ ^[0-9a-f]{40}$ ]]; then
  echo "release SHA must be a lowercase 40-character Git SHA" >&2
  exit 2
fi
if [[ ! "${artifact_sha256}" =~ ^[0-9a-f]{64}$ ]]; then
  echo "artifact SHA-256 must be lowercase hexadecimal" >&2
  exit 2
fi
if [[ "${artifact_uri}" != "${artifact_prefix}${release_sha}/amsonia-demo-web-${release_sha}.tar.gz" ]]; then
  echo "artifact URI and release SHA do not match" >&2
  exit 2
fi

install -d -m 0755 "${releases_root}"

work_dir="$(mktemp -d /var/tmp/amsonia-demo-web-deploy.XXXXXX)"
archive_path="${work_dir}/amsonia-demo-web.tar.gz"
stage_dir="${releases_root}/.${release_sha}.staging.$$"
release_dir="${releases_root}/${release_sha}"
previous_target=""
activated=0
succeeded=0

cleanup() {
  local exit_code=$?

  if [[ "${activated}" -eq 1 && "${succeeded}" -ne 1 && -n "${previous_target}" && -d "${previous_target}" ]]; then
    local rollback_link="${demo_root}/.current.rollback.$$"
    ln -s "${previous_target}" "${rollback_link}"
    mv -Tf "${rollback_link}" "${current_link}"
    systemctl restart "${service_name}" || true
  fi

  rm -rf "${work_dir}" "${stage_dir}"
  exit "${exit_code}"
}
trap cleanup EXIT

aws s3 cp "${artifact_uri}" "${archive_path}" --only-show-errors
printf '%s  %s\n' "${artifact_sha256}" "${archive_path}" | sha256sum --check --status

archive_entries="$(tar -tzf "${archive_path}")"
if [[ -z "${archive_entries}" ]]; then
  echo "Demo artifact is empty" >&2
  exit 1
fi

while IFS= read -r entry; do
  normalized="${entry%/}"
  if [[ -z "${normalized}" || "${normalized}" == /* || "${normalized}" == *\\* ]]; then
    echo "unsafe archive entry: ${entry}" >&2
    exit 1
  fi
  if [[ "/${normalized}/" == *"/../"* || "/${normalized}/" == *"/./"* ]]; then
    echo "unsafe archive entry: ${entry}" >&2
    exit 1
  fi
  case "${normalized}" in
    amsonia-static|web|web/*) ;;
    *)
      echo "unexpected archive entry: ${entry}" >&2
      exit 1
      ;;
  esac
done <<<"${archive_entries}"

grep -qx 'amsonia-static' <<<"${archive_entries}"
grep -qx 'web/index.html' <<<"${archive_entries}"
grep -qx 'web/robots.txt' <<<"${archive_entries}"
grep -qx 'web/amsonia-mark-v2.svg' <<<"${archive_entries}"

if [[ -d "${release_dir}" ]]; then
  test -x "${release_dir}/amsonia-static"
  test -s "${release_dir}/web/index.html"
  test "$(cat "${release_dir}/.artifact-sha256")" = "${artifact_sha256}"
else
  install -d -m 0755 "${stage_dir}"
  tar -xzf "${archive_path}" --directory "${stage_dir}" --no-same-owner --no-same-permissions
  unsafe_entry="$(find "${stage_dir}" ! -type d ! -type f -print -quit)"
  if [[ -n "${unsafe_entry}" ]]; then
    echo "artifact contains a non-regular entry: ${unsafe_entry}" >&2
    exit 1
  fi
  chown -R root:root "${stage_dir}"
  find "${stage_dir}" -type d -exec chmod 0755 {} +
  find "${stage_dir}/web" -type f -exec chmod 0644 {} +
  chmod 0755 "${stage_dir}/amsonia-static"
  printf '%s\n' "${artifact_sha256}" >"${stage_dir}/.artifact-sha256"
  chmod 0644 "${stage_dir}/.artifact-sha256"
  test -x "${stage_dir}/amsonia-static"
  test -s "${stage_dir}/web/index.html"
  test -s "${stage_dir}/web/robots.txt"
  test -s "${stage_dir}/web/amsonia-mark-v2.svg"
  test ! -e "${stage_dir}/web/sitemap.xml"
  grep -Fq 'content="#1d1b3f"' "${stage_dir}/web/index.html"
  grep -Fq 'href="/amsonia-mark-v2.svg"' "${stage_dir}/web/index.html"
  mv "${stage_dir}" "${release_dir}"
fi

previous_target="$(readlink -f "${current_link}" 2>/dev/null || true)"
next_link="${demo_root}/.current.next.$$"
ln -s "${release_dir}" "${next_link}"
mv -Tf "${next_link}" "${current_link}"
activated=1

systemctl restart "${service_name}"
systemctl is-active --quiet "${service_name}"

curl --fail --silent --show-error --retry 20 --retry-connrefused --retry-delay 1 \
  http://127.0.0.1:8083/healthz >/dev/null

for route in / /robots.txt /amsonia-mark-v2.svg; do
  status="$(curl --silent --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:8083${route}")"
  if [[ "${status}" != 200 ]]; then
    echo "unexpected local status for ${route}: ${status}" >&2
    exit 1
  fi
done

status="$(curl --silent --output /dev/null --write-out '%{http_code}' http://127.0.0.1:8083/sitemap.xml)"
if [[ "${status}" != 404 ]]; then
  echo "unexpected local status for /sitemap.xml: ${status}" >&2
  exit 1
fi

succeeded=1
echo "Amsonia Demo Console release ${release_sha} is active"
