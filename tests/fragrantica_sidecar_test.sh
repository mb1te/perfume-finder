#!/bin/sh

set -eu

test_id="$(date +%s)-$$"
image_name="perfume-finder-enricher-test-${test_id}"
container_name="perfume-finder-enricher-test-${test_id}"

cleanup() {
	docker rm -f "${container_name}" >/dev/null 2>&1 || true
	docker image rm -f "${image_name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker build --quiet --file Dockerfile.enricher --tag "${image_name}" . >/dev/null
docker run --detach \
	--name "${container_name}" \
	--read-only \
	--tmpfs /tmp:rw,noexec,nosuid,size=256m \
	--shm-size=128m \
	--cap-drop=ALL \
	--security-opt=no-new-privileges:true \
	--memory=512m \
	"${image_name}" >/dev/null

for attempt in 1 2 3 4 5 6 7 8 9 10; do
	if docker exec "${container_name}" /fragranticaenricher healthcheck >/dev/null 2>&1; then
		exit 0
	fi
	sleep 1
done

docker logs "${container_name}"
exit 1
