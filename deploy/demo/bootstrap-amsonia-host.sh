#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  echo "usage: bootstrap-amsonia-host.sh RELEASE_ID" >&2
  exit 2
fi

release_id="$1"
bundle_root="$(cd "$(dirname "$0")" && pwd)"
api_release="/opt/amsonia-core-demo/releases/${release_id}"
web_release="/opt/amsonia-core-demo-web/releases/${release_id}"
site_release="/opt/amsonia-site/releases/${release_id}"
database_root="/var/lib/pgsql/amsonia-core/data"

dnf install -y postgresql16 postgresql16-server postgresql16-contrib util-linux

if ! getent group amsonia-core >/dev/null; then
  groupadd --system amsonia-core
fi
if ! id -u amsonia-core >/dev/null 2>&1; then
  useradd --system --gid amsonia-core --home-dir /var/lib/amsonia-core --shell /sbin/nologin amsonia-core
fi
if ! getent group traefik >/dev/null; then
  groupadd --system traefik
fi
if ! id -u traefik >/dev/null 2>&1; then
  useradd --system --gid traefik --home-dir /var/lib/traefik --shell /sbin/nologin traefik
fi

install -d -m 0750 -o amsonia-core -g amsonia-core /var/lib/amsonia-core
install -d -m 0750 -o root -g amsonia-core /etc/amsonia-core
install -d -m 0755 "${api_release}" "${web_release}" "${site_release}"
install -d -m 0755 /etc/traefik/dynamic
install -d -m 0750 -o traefik -g traefik /var/lib/traefik
install -m 0600 -o traefik -g traefik /dev/null /var/lib/traefik/acme.json

install -m 0755 "${bundle_root}/amsonia-api" "${api_release}/amsonia-api"
install -m 0755 "${bundle_root}/amsonia" "${api_release}/amsonia"
install -m 0644 "${bundle_root}/configure_database_roles.sql" "${api_release}/configure_database_roles.sql"
install -m 0755 "${bundle_root}/amsonia-static" "${web_release}/amsonia-static"
install -m 0755 "${bundle_root}/amsonia-static" "${site_release}/amsonia-static"
cp -a "${bundle_root}/web" "${web_release}/web"
cp -a "${bundle_root}/site" "${site_release}/site"
chown -R root:root "${api_release}" "${web_release}" "${site_release}"

ln -sfn "${api_release}" /opt/amsonia-core-demo/current
ln -sfn "${web_release}" /opt/amsonia-core-demo-web/current
ln -sfn "${site_release}" /opt/amsonia-site/current

install -m 0755 "${bundle_root}/traefik" /usr/local/bin/traefik
install -m 0644 "${bundle_root}/traefik-static.yml" /etc/traefik/traefik.yml
install -m 0644 "${bundle_root}/traefik-demo-only.yml" /etc/traefik/dynamic/amsonia.yml
install -m 0644 "${bundle_root}/traefik.service" /etc/systemd/system/traefik.service
install -m 0644 "${bundle_root}/amsonia-core-postgres.service" /etc/systemd/system/amsonia-core-postgres.service
install -m 0644 "${bundle_root}/amsonia-core-api.service" /etc/systemd/system/amsonia-core-api.service
install -m 0644 "${bundle_root}/amsonia-core-web.service" /etc/systemd/system/amsonia-core-web.service
install -m 0644 "${bundle_root}/amsonia-site.service" /etc/systemd/system/amsonia-site.service

if [[ ! -s "${database_root}/PG_VERSION" ]]; then
  install -d -m 0700 -o postgres -g postgres "${database_root}"
  runuser -u postgres -- /usr/bin/initdb -D "${database_root}" --encoding=UTF8 --locale=C.UTF-8 --auth-local=peer --auth-host=scram-sha-256
  cat >>"${database_root}/postgresql.conf" <<'POSTGRES_CONFIG'
listen_addresses = '127.0.0.1'
port = 5433
password_encryption = 'scram-sha-256'
ssl = off
POSTGRES_CONFIG
  cat >"${database_root}/pg_hba.conf" <<'POSTGRES_HBA'
local   all             postgres                                peer
local   all             all                                     peer
host    all             all             127.0.0.1/32            scram-sha-256
host    all             all             ::1/128                 scram-sha-256
POSTGRES_HBA
  chown postgres:postgres "${database_root}/postgresql.conf" "${database_root}/pg_hba.conf"
  chmod 0600 "${database_root}/postgresql.conf" "${database_root}/pg_hba.conf"
fi

systemctl daemon-reload
systemctl enable --now amsonia-core-postgres.service

if [[ ! -s /etc/amsonia-core/demo.env ]]; then
  owner_password="$(openssl rand -hex 32)"
  runtime_password="$(openssl rand -hex 32)"
  maintenance_password="$(openssl rand -hex 32)"
  runtime_secret_hex="$(openssl rand -hex 32)"
  maintenance_secret_hex="$(openssl rand -hex 32)"
  runtime_binding_secret="$(RUNTIME_SECRET_HEX="${runtime_secret_hex}" python3 -c 'import base64,os; print(base64.urlsafe_b64encode(bytes.fromhex(os.environ["RUNTIME_SECRET_HEX"])).decode().rstrip("="))')"

  runuser -u postgres -- psql -p 5433 -d postgres -v ON_ERROR_STOP=1 \
    -v owner_password="${owner_password}" \
    -v runtime_password="${runtime_password}" \
    -v maintenance_password="${maintenance_password}" <<'ROLE_SQL'
SELECT format('CREATE ROLE amsonia_owner LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L', :'owner_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'amsonia_owner')
\gexec
SELECT format('CREATE ROLE amsonia_runtime LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L', :'runtime_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'amsonia_runtime')
\gexec
SELECT format('CREATE ROLE amsonia_maintenance LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE NOBYPASSRLS PASSWORD %L', :'maintenance_password')
WHERE NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = 'amsonia_maintenance')
\gexec
SELECT 'CREATE DATABASE amsonia_core OWNER amsonia_owner'
WHERE NOT EXISTS (SELECT 1 FROM pg_database WHERE datname = 'amsonia_core')
\gexec
ROLE_SQL

  export AMSONIA_MIGRATION_DSN="postgres://amsonia_owner:${owner_password}@127.0.0.1:5433/amsonia_core?sslmode=disable"
  "${api_release}/amsonia" migrate
  runuser -u postgres -- psql -p 5433 -d amsonia_core -v ON_ERROR_STOP=1 \
    -v runtime_role=amsonia_runtime \
    -v maintenance_role=amsonia_maintenance \
    -v runtime_secret_hex="${runtime_secret_hex}" \
    -v maintenance_secret_hex="${maintenance_secret_hex}" \
    -f "${api_release}/configure_database_roles.sql"

  umask 0027
  printf '%s\n' \
    "AMSONIA_DATABASE_DSN=postgres://amsonia_runtime:${runtime_password}@127.0.0.1:5433/amsonia_core?sslmode=disable" \
    "AMSONIA_TENANT_BINDING_SECRET=${runtime_binding_secret}" \
    'AMSONIA_HTTP_ADDR=127.0.0.1:8082' \
    >/etc/amsonia-core/demo.env
  chown root:amsonia-core /etc/amsonia-core/demo.env
  chmod 0640 /etc/amsonia-core/demo.env
fi

if ! runuser -u postgres -- psql -p 5433 -d amsonia_core -Atqc 'SELECT EXISTS (SELECT 1 FROM amsonia.system_administrators)' | grep -qx t; then
  admin_json="$(aws ssm get-parameter --name /amsonia/prod/demo-admin --with-decryption --region us-east-1 --query Parameter.Value --output text)"
  admin_email="$(ADMIN_JSON="${admin_json}" python3 -c 'import json,os; print(json.loads(os.environ["ADMIN_JSON"])["email"])')"
  admin_password="$(ADMIN_JSON="${admin_json}" python3 -c 'import json,os; print(json.loads(os.environ["ADMIN_JSON"])["password"])')"
  set -a
  source /etc/amsonia-core/demo.env
  set +a
  printf '%s\n%s\n' "${admin_email}" "${admin_password}" | script -q -e -c "${api_release}/amsonia bootstrap-admin" /dev/null >/dev/null
  unset admin_json admin_email admin_password
fi

viewer_json="$(aws ssm get-parameter --name /amsonia/prod/demo-viewer --with-decryption --region us-east-1 --query Parameter.Value --output text)"
viewer_email="$(VIEWER_JSON="${viewer_json}" python3 -c 'import json,os; print(json.loads(os.environ["VIEWER_JSON"])["email"])')"
viewer_password="$(VIEWER_JSON="${viewer_json}" python3 -c 'import json,os; print(json.loads(os.environ["VIEWER_JSON"])["password"])')"
set -a
source /etc/amsonia-core/demo.env
set +a
AMSONIA_DEMO_VIEWER_EMAIL="${viewer_email}" \
AMSONIA_DEMO_VIEWER_PASSWORD="${viewer_password}" \
AMSONIA_DEMO_TENANT_NAME="Amsonia Core Demo" \
  "${api_release}/amsonia" provision-demo-viewer
unset viewer_json viewer_email viewer_password

systemctl enable --now amsonia-core-api.service amsonia-core-web.service amsonia-site.service traefik.service
systemctl restart amsonia-core-api.service amsonia-core-web.service amsonia-site.service traefik.service

for url in \
  http://127.0.0.1:8082/health \
  http://127.0.0.1:8082/readyz \
  http://127.0.0.1:8083/healthz \
  http://127.0.0.1:8084/healthz; do
  curl --fail --silent --show-error --retry 20 --retry-connrefused --retry-delay 1 "${url}" >/dev/null
done

test "$(curl --silent --output /dev/null --write-out '%{http_code}' http://127.0.0.1:8083/sitemap.xml)" = 404
test "$(curl --silent --output /dev/null --write-out '%{http_code}' http://127.0.0.1:8084/api/example)" = 404

redirect_status="$(curl --silent --output /dev/null --write-out '%{http_code}' \
  --retry 20 --retry-connrefused --retry-delay 1 \
  -H 'Host: demo.amsonia.dev' http://127.0.0.1/)"
if [[ "${redirect_status}" != 301 && "${redirect_status}" != 308 ]]; then
  echo "unexpected origin redirect status: ${redirect_status}" >&2
  exit 1
fi

echo "Amsonia host ${release_id} is healthy"
