#!/bin/bash
set -euo pipefail
mkdir -p ~/.local/bin
cp "/Users/ai/tutti/tools/agent-bridges/doubao-bridge.mjs" ~/.local/bin/tutti-doubao-bridge
chmod +x ~/.local/bin/tutti-doubao-bridge
echo "已安装 ~/.local/bin/tutti-doubao-bridge"
