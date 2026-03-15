#!/usr/bin/env bash
# 一鍵跑 Chatmery（Go 版）。預設：systemctl --user start chatmery，確認後台跑起來後提醒可關閉終端。
# 需先執行一次 --install 安裝開機自啟。加 --service 為 systemd 內部用，勿手動傳。
set -e
cd "$(dirname "$0")"
CHATMERY_DIR="$(pwd)"
# 精準日期時間，所有輸出都帶上
ts() { date '+%Y-%m-%d %H:%M:%S'; }

# --install：設定開機自啟（寫入 systemd user 服務並 enable）
if [[ "$1" == "--install" ]]; then
  mkdir -p ~/.config/systemd/user
  cat << EOF > ~/.config/systemd/user/chatmery.service
[Unit]
Description=Chatmery Telegram + Ollama bot
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
WorkingDirectory=$CHATMERY_DIR
ExecStart=$CHATMERY_DIR/run.sh --service
Restart=on-failure
RestartSec=10

[Install]
WantedBy=default.target
EOF
  systemctl --user daemon-reload
  systemctl --user enable chatmery
  echo "[$(ts)] 已設定開機自啟。之後直接執行 ./run.sh 即會以後台啟動並提示可關閉終端。"
  exit 0
fi

# 無參數：由 systemd 在後台跑，確認啟動後提醒可關閉終端
if [[ -z "$1" ]]; then
  if ! systemctl --user start chatmery 2>/dev/null; then
    echo "[$(ts)] 請先執行 ./run.sh --install 安裝開機自啟，再執行 ./run.sh"
    exit 1
  fi
  sleep 2
  if systemctl --user is-active --quiet chatmery 2>/dev/null; then
    echo "[$(ts)] Chatmery 已在後台執行，可關閉終端。查狀態：systemctl --user status chatmery"
  else
    echo "[$(ts)] 啟動中，可關閉終端。若異常請查：systemctl --user status chatmery"
  fi
  exit 0
fi

# 以下為 --service（systemd 呼叫）或 --background 時執行
# 確保只跑一個實例：--service 時只殺既有 go 行程；非 --service 時先停 systemd 再殺
if [[ "$1" != "--service" ]]; then
  systemctl --user stop chatmery 2>/dev/null || true
fi
pkill -f "cmd/chatmery" 2>/dev/null || true
pkill -x chatmery 2>/dev/null || true
sleep 1

if [[ -f .env ]]; then
  set -a
  # shellcheck source=/dev/null
  source .env
  set +a
  echo "[$(ts)] 已載入 .env"
else
  echo "[$(ts)] 提示：可複製 .env.example 為 .env 並填入 TELEGRAM_BOT_TOKEN 等，避免寫在程式裡"
fi

if [[ -z "$TELEGRAM_BOT_TOKEN" && -z "$CHATMERY_TELEGRAM_TOKEN" ]]; then
  echo "[$(ts)] 錯誤：請設 TELEGRAM_BOT_TOKEN 或 CHATMERY_TELEGRAM_TOKEN（寫在 .env 或 export）"
  exit 1
fi

# --background：不用 systemd，直接 nohup 後台跑（未 --install 時可用）
if [[ "$1" == "--background" ]]; then
  nohup go run ./cmd/chatmery >> chatmery.log 2>&1 &
  sleep 2
  if pgrep -f "cmd/chatmery" >/dev/null; then
    echo "[$(ts)] Chatmery 已在後台執行，可關閉終端。log：$CHATMERY_DIR/chatmery.log"
  else
    echo "[$(ts)] 啟動可能失敗，請檢查 chatmery.log"
    exit 1
  fi
  exit 0
fi

# --service：systemd 呼叫，真正跑 bot（擋住直到結束）
exec go run ./cmd/chatmery
