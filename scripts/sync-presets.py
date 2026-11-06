#!/usr/bin/env python3
"""从 magpie 同步供应商预设表。

[INPUT]: 读取本地预设 JSON。
[OUTPUT]: 生成并 gofmt 格式化 Go 预设表。
[POS]: 离线数据生成入口。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md

用法：
    # 1. 先在一个 magpie 的检出里导出 JSON
    cd /path/to/magpie
    mkdir -p cmd/dumppresets && cat > cmd/dumppresets/main.go <<'GO'
package main

import (
    "encoding/json"
    "os"

    "github.com/yetone/magpie/internal/provider"
)

func main() {
    enc := json.NewEncoder(os.Stdout)
    enc.SetIndent("", "  ")
    enc.Encode(provider.Presets())
}
GO
    go run ./cmd/dumppresets > /tmp/magpie_presets.json

    # 2. 回到 konseki 生成 Go 源码
    python3 scripts/sync-presets.py /tmp/magpie_presets.json

为什么要有这个脚本：预设表是「事实数据」，跟着上游走才不会漂移。
手抄一次能对，改三次就对不上了。
"""

import json
import sys
import subprocess


def golit(s):
    return json.dumps(s, ensure_ascii=False)


def main():
    src = sys.argv[1] if len(sys.argv) > 1 else "/tmp/magpie_presets.json"
    raw = json.load(open(src))

    # magpie 里有几个预设只做路由决策（decide），不提供对话能力。
    # Konseki 只关心能对话的，那些剔掉。
    kept = []
    for p in raw:
        has_talk = bool(p.get("chat") or p.get("anthropic") or p.get("endpoint"))
        if p.get("regions"):
            good = [r for r in p["regions"]
                    if r.get("chat") or r.get("anthropic") or has_talk]
            if not good:
                continue
            p["regions"] = good
            has_talk = True
        if not has_talk:
            continue
        kept.append(p)

    lines = [
        "package providers",
        "",
        "// 预设表：从 magpie 的 internal/provider/presets.go 导出，不手抄。",
        "//",
        "// 不要直接改这个文件 —— 下次同步会被覆盖。改上游，或者改",
        "// Konseki 自己对它的消费方式（见 ui_provider.go）。",
        "//",
        "// 重新生成：见 scripts/sync-presets.py 的说明。",
        "func init() {",
        "\tpresets = []Preset{",
    ]

    for p in kept:
        lines.append("\t\t{")
        lines.append(f'\t\t\tID: {golit(p["id"])},')
        lines.append(f'\t\t\tName: {golit(p.get("name", p["id"]))},')
        if p.get("short"):
            lines.append(f'\t\t\tShort: {golit(p["short"])},')
        lines.append(f'\t\t\tKind: {golit(p.get("kind", "vendor"))},')
        if p.get("icon"):
            lines.append(f'\t\t\tIcon: {golit(p["icon"])},')
        for key, field in [("chat", "Chat"), ("responses", "Responses"),
                           ("anthropic", "Anthropic"), ("note", "Note"),
                           ("website", "Website"), ("keysUrl", "KeysURL"),
                           ("keyHint", "KeyHint"), ("only", "Only"),
                           ("endpoint", "Endpoint"), ("endpointHint", "EndpointHint"),
                           ("regionLabel", "RegionLabel")]:
            if p.get(key):
                lines.append(f'\t\t\t{field}: {golit(p[key])},')
        if p.get("models"):
            ms = ", ".join(golit(m) for m in p["models"])
            lines.append(f'\t\t\tModels: []string{{{ms}}},')
        if p.get("noKey"):
            lines.append("\t\t\tNoKey: true,")
        if p.get("noList"):
            lines.append("\t\t\tNoList: true,")
        if p.get("headerHints"):
            hs = ", ".join(golit(h) for h in p["headerHints"])
            lines.append(f'\t\t\tHeaderHints: []string{{{hs}}},')
        if p.get("regions"):
            lines.append("\t\t\tRegions: []Region{")
            for r in p["regions"]:
                parts = [f'ID: {golit(r["id"])}']
                if r.get("name"):
                    parts.append(f'Name: {golit(r["name"])}')
                for key, field in [("chat", "Chat"), ("responses", "Responses"),
                                   ("anthropic", "Anthropic"), ("website", "Website"),
                                   ("keysUrl", "KeysURL")]:
                    if r.get(key):
                        parts.append(f'{field}: {golit(r[key])}')
                if r.get("models"):
                    ms = ", ".join(golit(m) for m in r["models"])
                    parts.append(f"Models: []string{{{ms}}}")
                lines.append("\t\t\t\t{" + ", ".join(parts) + "},")
            lines.append("\t\t\t},")
        lines.append("\t\t},")

    lines += ["\t}", "}"]
    out = "\n".join(lines) + "\n"

    dest = "internal/providers/presets.go"
    open(dest, "w").write(out)
    subprocess.run(["gofmt", "-w", dest], check=True)
    print(f"wrote {dest}: {len(kept)} presets ({len(raw) - len(kept)} decide-only dropped)")


if __name__ == "__main__":
    main()
