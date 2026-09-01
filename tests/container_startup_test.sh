#!/bin/sh

set -eu

test_id="$(date +%s)-$$"
image_name="perfume-finder-startup-test-${test_id}"
container_name="perfume-finder-volume-seed-${test_id}"
volume_name="perfume-finder-startup-test-${test_id}"

cleanup() {
	docker rm -f "${container_name}" >/dev/null 2>&1 || true
	docker volume rm -f "${volume_name}" >/dev/null 2>&1 || true
	docker image rm -f "${image_name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT INT TERM

docker build --quiet --tag "${image_name}" . >/dev/null
docker volume create "${volume_name}" >/dev/null
docker create \
	--name "${container_name}" \
	--mount "type=volume,source=${volume_name},target=/data" \
	"${image_name}" healthcheck >/dev/null

docker run --rm \
	--user 65532:65532 \
	--mount "type=volume,source=${volume_name},target=/data" \
	alpine:3.23 touch /data/startup-probe
