#!/usr/bin/env bash
# SPDX-License-Identifier: MPL-2.0

set -euo pipefail

cd "$(dirname "$0")"

use_docker="${WIPPY_CDC_TEST_DOCKER:-0}"
for arg in "$@"; do
	case "$arg" in
		--docker)
			use_docker=1
			;;
		--help|-h)
			echo "usage: $0 [--docker]"
			echo
			echo "Default: run focused CDC unit tests and compile integration tests."
			echo "--docker: start a temporary Postgres with logical replication and run live CDC integration tests."
			exit 0
			;;
		*)
			echo "unknown argument: $arg" >&2
			echo "usage: $0 [--docker]" >&2
			exit 2
			;;
	esac
done

go test ./api/service/cdc ./service/cdc/postgres ./service/cdc/sqlite ./runtime/lua/modules/cdc ./boot/components/dispatchers

echo "running sqlite cdc integration tests (local temp file, no docker)"
CGO_ENABLED=1 go test -tags "integration sqlite_preupdate_hook" ./service/cdc/sqlite

if [[ -n "${WIPPY_CDC_IT_REPL_DSN:-}" && -n "${WIPPY_CDC_IT_ADMIN_DSN:-}" ]]; then
	go test -tags integration ./service/cdc/postgres
	exit 0
fi

if [[ "$use_docker" != "1" ]]; then
	echo "WIPPY_CDC_IT_REPL_DSN/WIPPY_CDC_IT_ADMIN_DSN not set; running CDC integration compile check only."
	echo "Run '$0 --docker' for a live logical-replication Postgres proof."
	go test -tags integration -run '^$' ./service/cdc/postgres
	exit 0
fi

container="wippy-cdc-it-$$"
cleanup() {
	if [[ "${WIPPY_CDC_KEEP_DOCKER:-0}" != "1" ]]; then
		docker rm -f "$container" >/dev/null 2>&1 || true
	fi
}
trap cleanup EXIT

docker run -d \
	--name "$container" \
	--label ai.wippy.owner=wippy \
	--label ai.wippy.cdc-test=1 \
	-e POSTGRES_PASSWORD=postgres \
	-e POSTGRES_DB=wippy \
	-p 127.0.0.1::5432 \
	postgres:17 \
	-c wal_level=logical \
	-c max_wal_senders=16 \
	-c max_replication_slots=32 >/dev/null

for _ in {1..60}; do
	if docker exec "$container" pg_isready -U postgres -d wippy >/dev/null 2>&1; then
		break
	fi
	sleep 1
done

docker exec "$container" pg_isready -U postgres -d wippy >/dev/null
docker exec "$container" psql -U postgres -d wippy -v ON_ERROR_STOP=1 \
	-c "CREATE ROLE cdc_repl WITH REPLICATION LOGIN PASSWORD 'cdc_repl';" \
	-c "GRANT ALL PRIVILEGES ON DATABASE wippy TO cdc_repl;" >/dev/null

port="$(docker port "$container" 5432/tcp | sed -n 's/.*://p' | head -n1)"
export WIPPY_CDC_IT_ADMIN_DSN="postgres://postgres:postgres@127.0.0.1:${port}/wippy?sslmode=disable"
export WIPPY_CDC_IT_REPL_DSN="postgres://cdc_repl:cdc_repl@127.0.0.1:${port}/wippy?sslmode=disable&replication=database"

go test -tags integration ./service/cdc/postgres
