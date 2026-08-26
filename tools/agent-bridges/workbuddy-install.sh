#!/bin/bash
set -euo pipefail
REPO_DIR="/Users/ai/tutti"
BRIDGE_SRC="$REPO_DIR/tools/agent-bridges/workbuddy-bridge.mjs"

mkdir -p ~/.local/bin
cp "$BRIDGE_SRC" ~/.local/bin/tutti-workbuddy-bridge
chmod +x ~/.local/bin/tutti-workbuddy-bridge

if [ ! -x "$HOME/.local/share/tutti/codebuddy-standalone/node_modules/.bin/codebuddy" ]; then
  npm install --prefix ~/.local/share/tutti/codebuddy-standalone @tencent-ai/codebuddy-code@2.121.2
fi

printf '%s\n' '#!/bin/bash' 'REAL="$HOME/.local/share/tutti/codebuddy-standalone/node_modules/.bin/codebuddy"' 'if [ "$1" = "login" ] || [ "$1" = "--login" ]; then' '  echo "即将进入 CodeBuddy 交互界面，请输入 /login 完成扫码登录，退出输入 /quit。"' '  exec "$REAL"' 'fi' 'exec "$REAL" "$@"' > ~/.local/bin/codebuddy
chmod +x ~/.local/bin/codebuddy

echo "已安装 tutti-workbuddy-bridge 与 codebuddy CLI 包装器"
