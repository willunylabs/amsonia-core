#!/usr/bin/env bash
set -Eeuo pipefail

if [[ "${EUID}" -ne 0 ]]; then
  echo "deploy-amsonia-site must run as root" >&2
  exit 1
fi

if [[ "$#" -ne 3 ]]; then
  echo "usage: deploy-amsonia-site S3_ARTIFACT_URI RELEASE_SHA ARTIFACT_SHA256" >&2
  exit 2
fi

artifact_uri="$1"
release_sha="$2"
artifact_sha256="$3"

artifact_prefix="s3://h1-recon-results-336090301244-us-east-1/deployments/amsonia/site/"
site_root="/opt/amsonia-site"
releases_root="${site_root}/releases"
current_link="${site_root}/current"
service_name="amsonia-site.service"

if [[ ! "${artifact_uri}" =~ ^${artifact_prefix}[0-9a-f]{40}/amsonia-site-[0-9a-f]{40}\.tar\.gz$ ]]; then
  echo "artifact URI is outside the approved Amsonia site prefix" >&2
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
if [[ "${artifact_uri}" != "${artifact_prefix}${release_sha}/amsonia-site-${release_sha}.tar.gz" ]]; then
  echo "artifact URI and release SHA do not match" >&2
  exit 2
fi

install -d -m 0755 "${releases_root}"

work_dir="$(mktemp -d /var/tmp/amsonia-site-deploy.XXXXXX)"
archive_path="${work_dir}/amsonia-site.tar.gz"
stage_dir="${releases_root}/.${release_sha}.staging.$$"
release_dir="${releases_root}/${release_sha}"
previous_target=""
activated=0
succeeded=0

cleanup() {
  local exit_code=$?

  if [[ "${activated}" -eq 1 && "${succeeded}" -ne 1 && -n "${previous_target}" && -d "${previous_target}" ]]; then
    local rollback_link="${site_root}/.current.rollback.$$"
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
  echo "site artifact is empty" >&2
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
    amsonia-static|site|site/*) ;;
    *)
      echo "unexpected archive entry: ${entry}" >&2
      exit 1
      ;;
  esac
done <<<"${archive_entries}"

grep -qx 'amsonia-static' <<<"${archive_entries}"
grep -qx 'site/index.html' <<<"${archive_entries}"
grep -qx 'site/robots.txt' <<<"${archive_entries}"
grep -qx 'site/sitemap.xml' <<<"${archive_entries}"

if [[ -d "${release_dir}" ]]; then
  test -x "${release_dir}/amsonia-static"
  test -s "${release_dir}/site/index.html"
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
  find "${stage_dir}/site" -type f -exec chmod 0644 {} +
  chmod 0755 "${stage_dir}/amsonia-static"
  printf '%s\n' "${artifact_sha256}" >"${stage_dir}/.artifact-sha256"
  chmod 0644 "${stage_dir}/.artifact-sha256"
  test -x "${stage_dir}/amsonia-static"
  test -s "${stage_dir}/site/index.html"
  mv "${stage_dir}" "${release_dir}"
fi

previous_target="$(readlink -f "${current_link}" 2>/dev/null || true)"
next_link="${site_root}/.current.next.$$"
ln -s "${release_dir}" "${next_link}"
mv -Tf "${next_link}" "${current_link}"
activated=1

systemctl restart "${service_name}"
systemctl is-active --quiet "${service_name}"

curl --fail --silent --show-error --retry 20 --retry-connrefused --retry-delay 1 \
  http://127.0.0.1:8084/healthz >/dev/null

for route in / /core /core/docs/getting-started /robots.txt /sitemap.xml; do
  status="$(curl --silent --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:8084${route}")"
  if [[ "${status}" != 200 ]]; then
    echo "unexpected local status for ${route}: ${status}" >&2
    exit 1
  fi
done

for route in /api/example /deployment-probe-not-found; do
  status="$(curl --silent --output /dev/null --write-out '%{http_code}' "http://127.0.0.1:8084${route}")"
  if [[ "${status}" != 404 ]]; then
    echo "unexpected local status for ${route}: ${status}" >&2
    exit 1
  fi
done

succeeded=1
echo "Amsonia site release ${release_sha} is active"
