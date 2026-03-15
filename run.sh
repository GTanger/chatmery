#!/usr/bin/env bash
# 一鍵跑 Chatmery（Go 版）。會載入 .env，敏感資訊勿寫進程式碼。
set -e
cd "$(dirname "$0")"

if [[ -f .env ]]; then
  set -a
  # shellcheck source=/dev/null
  source .env
  set +a
  echo "已載入 .env"
else
  echo "提示：可複製 .env.example 為 .env 並填入 TELEGRAM_BOT_TOKEN 等，避免寫在程式裡"
fi

if [[ -z "$TELEGRAM_BOT_TOKEN" && -z "$CHATMERY_TELEGRAM_TOKEN" ]]; then
  echo "錯誤：請設 TELEGRAM_BOT_TOKEN 或 CHATMERY_TELEGRAM_TOKEN（寫在 .env 或 export）"
  exit 1
fi

exec go run ./cmd/chatmery
