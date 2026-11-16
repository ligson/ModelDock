#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
STATE_DIR="${MODELDOCK_DEV_STATE_DIR:-$ROOT_DIR/.modeldock-dev}"
PID_FILE="$STATE_DIR/modeldock.pid"
LOG_FILE="$STATE_DIR/modeldock.log"
PORT="${MODELDOCK_DEV_PORT:-8787}"
MODEL_ROOT="${MODELDOCK_MODEL_ROOT:-$HOME/ModelDock}"

usage() { printf 'Usage: %s {start|stop|restart|status}\n' "$(basename "$0")"; }
is_running() { [[ -f "$PID_FILE" ]] && kill -0 "$(cat "$PID_FILE")" 2>/dev/null; }

start() {
  if is_running; then echo "ModelDock 已运行（PID $(cat "$PID_FILE")，端口 $PORT）"; return 0; fi
  mkdir -p "$STATE_DIR" "$MODEL_ROOT"
  go build -o "$STATE_DIR/modeldock" ./cmd/modeldock
  nohup "$STATE_DIR/modeldock" serve --addr "127.0.0.1:$PORT" --root "$MODEL_ROOT" >"$LOG_FILE" 2>&1 &
  echo $! >"$PID_FILE"
  sleep 0.3
  if ! is_running; then echo "ModelDock 启动失败，查看 $LOG_FILE" >&2; return 1; fi
  echo "ModelDock 已启动：http://127.0.0.1:$PORT"
}

stop() {
  if ! [[ -f "$PID_FILE" ]]; then echo "ModelDock 未运行"; return 0; fi
  pid="$(cat "$PID_FILE")"
  if kill -0 "$pid" 2>/dev/null; then kill "$pid"; for _ in {1..20}; do kill -0 "$pid" 2>/dev/null || break; sleep 0.1; done; fi
  rm -f "$PID_FILE"
  echo "ModelDock 已停止"
}

status() {
  if is_running; then echo "ModelDock 运行中（PID $(cat "$PID_FILE")，端口 ${PORT}）"; curl -fsS "http://127.0.0.1:${PORT}/api/health"; printf '\n'; else echo "ModelDock 未运行"; return 1; fi
}

case "${1:-}" in start) start ;; stop) stop ;; restart) stop; start ;; status) status ;; *) usage; exit 2 ;; esac
