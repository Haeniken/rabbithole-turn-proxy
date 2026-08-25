#!/bin/sh
set -e

CONNECT="${CONNECT_ADDR:?CONNECT_ADDR is required}"
LISTEN="${LISTEN_ADDR:-0.0.0.0:56000}"
DRAIN_TIMEOUT="${DRAIN_TIMEOUT:-30s}"
METRICS_LISTEN="${METRICS_LISTEN:-127.0.0.1:9090}"
INSTANCE_NAME="${INSTANCE_NAME:-default}"
MAX_SESSIONS="${MAX_SESSIONS:-2048}"
MAX_HANDSHAKES="${MAX_HANDSHAKES:-128}"
MAX_GOROUTINES="${MAX_GOROUTINES:-20000}"
MIN_FREE_FDS="${MIN_FREE_FDS:-128}"
CANARY_MAX_SESSIONS="${CANARY_MAX_SESSIONS:-64}"

set -- ./vk-turn-proxy \
    -listen "$LISTEN" \
    -connect "$CONNECT" \
    -drain-timeout "$DRAIN_TIMEOUT" \
    -metrics-listen "$METRICS_LISTEN" \
    -instance-name "$INSTANCE_NAME" \
    -max-sessions "$MAX_SESSIONS" \
    -max-handshakes "$MAX_HANDSHAKES" \
    -max-goroutines "$MAX_GOROUTINES" \
    -min-free-fds "$MIN_FREE_FDS" \
    -canary-max-sessions "$CANARY_MAX_SESSIONS"

if [ "${VLESS_MODE}" = "true" ]; then
    set -- "$@" -vless
fi

if [ "${VLESS_BOND}" = "true" ]; then
    set -- "$@" -vless-bond
fi

if [ "${WRAP_MODE}" = "true" ]; then
    WRAP="${WRAP_KEY:?WRAP_KEY is required when WRAP_MODE=true}"
    set -- "$@" -wrap -wrap-key "$WRAP"
fi

if [ "${CANARY_MODE}" = "true" ]; then
    set -- "$@" -canary
fi

if [ "${DEBUG_MODE}" = "true" ]; then
    set -- "$@" -debug
fi

exec "$@"
