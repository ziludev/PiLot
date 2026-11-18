#!/bin/bash
# manual-check.sh runs the four input checks the goal asks a person to make in
# the real window: IME, Shift+Enter, selection, clipboard. It captures one
# screenshot per step so the result is a record, not a memory.
#
# Usage: scripts/manual-check.sh [out-dir]   (default docs/manual-checks)
set -u

out=${1:-docs/manual-checks}
mkdir -p "$out"

window_rect() {
  osascript -e 'tell application "System Events" to tell process "PiLot" to get {position, size} of window 1' \
    2>/dev/null | tr -d ' ' | awk -F, '{print $1","$2","$3","$4}'
}

shot() {
  local name=$1 rect
  rect=$(window_rect)
  if [ -z "$rect" ]; then
    echo "cannot find the PiLot window; open ~/Applications/PiLot.app first" >&2
    exit 1
  fi
  sleep 0.6
  screencapture -x -R"$rect" "$out/$name.png"
  echo "  saved $out/$name.png"
}

step() {
  local name=$1 text=$2
  echo
  echo "== $name"
  echo "   $text"
  read -r -p "   press Enter once you have done it, or type 'fail' and Enter to record a failure: " answer
  if [ "$answer" = "fail" ]; then
    echo "$name: FAIL" >>"$out/results.txt"
    shot "$name-fail"
    return
  fi
  echo "$name: ok" >>"$out/results.txt"
  shot "$name"
}

echo "This writes into $out and takes four screenshots of the PiLot window."
echo "Keep the PiLot window visible; do not type into the terminal during a step."
: >"$out/results.txt"

step 1-ime "在输入框里用中文输入法打 'nihao'，让候选框弹出、先不要上屏；按 Enter 一次（应既不发送也不换行，拼音仍在候选状态）；再按空格上屏，然后按 ⌘Enter 发送。"
step 2-shift-enter "输入 'line-one'，按 Shift+Enter，再输入 'line-two'——两行应留在同一个草稿里，没有发送。"
step 3-selection "把刚写的两行全选（⌘A），输入 'replaced'——整段被替换成一个词。"
step 4-clipboard "在终端或任意编辑器复制一段文字，回到输入框按 ⌘V——粘贴进来的是那段文字。"

echo
echo "results:"
cat "$out/results.txt"
echo
echo "Send $out/results.txt and the four pngs back; they are the manual record."
