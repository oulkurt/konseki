#!/bin/sh
# [INPUT]: 依赖 docs/icon.svg 的图标母版；依赖本机 Chrome 的无头渲染，可用 CHROME 指定别的 Chromium
# [OUTPUT]: 写出 resources/icon.png 和 resources/icon-dev.png，都是 1024x1024，透明底
# [POS]: scripts 的图标渲染脚本。mygo build 用 icon.png 生成 .icns，mygo dev 用 icon-dev.png
# [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
set -eu
cd "$(dirname "$0")/.."

# ============ 母版必须是合法 XML：Chrome 会把解析错误页也截成 PNG ============
xmllint --noout docs/icon.svg

chrome="${CHROME:-/Applications/Google Chrome.app/Contents/MacOS/Google Chrome}"
if [ ! -x "$chrome" ]; then
  echo "render-icon: 找不到 Chrome（${chrome}），用 CHROME 指定 Chromium 的可执行文件" >&2
  exit 1
fi
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

# ============ 渲染到临时文件，尺寸必须是 1024x1024 ============
render() {
  "$chrome" --headless=new --disable-gpu --hide-scrollbars \
    --force-device-scale-factor=1 --default-background-color=00000000 \
    --window-size=1024,1024 --screenshot="$tmp/$2" \
    "file://$PWD/docs/icon.svg$1" 2>/dev/null
  size="$(sips -g pixelWidth -g pixelHeight "$tmp/$2" 2>/dev/null | awk '/pixel/ {print $2}' | tr '\n' 'x')"
  if [ "$size" != "1024x1024x" ]; then
    echo "render-icon: Chrome 没有写出 1024x1024 的 ${2}（得到 ${size:-无文件}）" >&2
    exit 1
  fi
}
render "" icon.png
render "#dev" icon-dev.png

# ============ 开发版必须和正式版不同：#dev 没命中 :target 时两张图一样 ============
if cmp -s "$tmp/icon.png" "$tmp/icon-dev.png"; then
  echo "render-icon: icon.svg#dev 没有换出开发版配色" >&2
  exit 1
fi

# ============ 两张都通过才替换 ============
mv "$tmp/icon.png" resources/icon.png
mv "$tmp/icon-dev.png" resources/icon-dev.png
echo "resources/icon.png"
echo "resources/icon-dev.png"
