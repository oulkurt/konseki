# skill/
> L2 | 父级: ../CLAUDE.md
CLAUDE.md: 描述本目录的职责和成员。
legacy.go: Home 缺失时 LegacyDir 返回空串并安全空转；按旧导出器的固定标记识别 SKILL.md、KONSEKI.md 和真实 days 目录，统一容忍不存在错误，只删除旧文件，仅在找到旧文件时清理空真实目录，清理尾随斜杠并保留软链接。
legacy_test.go: 用 testdata/legacy 验证识别、删除边界、空目录保护、坏链接与非目录 days 跳过、空 Home 安全空转、新技能与用户文件保留，以及含尾随斜杠的软链接目录。
testdata/legacy/: 99394ed 的导出器生成的样本，作为旧文件识别基准。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
