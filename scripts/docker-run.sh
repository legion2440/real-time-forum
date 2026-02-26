#!/usr/bin/env bash
set -euo pipefail

IMAGE="${IMAGE:-forum:local}"
CONTAINER="${CONTAINER:-forum-local}"
HOST_PORT="${HOST_PORT:-8080}"

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
PROJECT_ROOT="$(cd -- "$SCRIPT_DIR/.." && pwd)"

cleanup() {
  docker stop "$CONTAINER" >/dev/null 2>&1 || true
  docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
}

trap cleanup EXIT

cd "$PROJECT_ROOT"

docker rm -f "$CONTAINER" >/dev/null 2>&1 || true

echo "==> docker build --progress=plain -t $IMAGE ."
docker build --progress=plain -t "$IMAGE" .

echo "==> docker run --rm -d --name $CONTAINER -p ${HOST_PORT}:8080 $IMAGE"
cid="$(docker run --rm -d --name "$CONTAINER" -p "${HOST_PORT}:8080" "$IMAGE")"
[ -n "$cid" ]

echo "==> docker ps (container row)"
docker ps --filter "name=$CONTAINER" --format "table {{.ID}}\t{{.Image}}\t{{.Status}}\t{{.Ports}}\t{{.Names}}"

url="http://127.0.0.1:${HOST_PORT}/"
ok=0
for _ in $(seq 1 20); do
  code="$(curl -sS -o /dev/null -w '%{http_code}' "$url" || true)"
  if [ "$code" = "200" ]; then
    ok=1
    break
  fi
  sleep 0.5
done

if [ "$ok" -ne 1 ]; then
  echo "HTTP check failed (expected 200 from $url)" >&2
  exit 1
fi

echo "==> HTTP check passed: 200 $url"
