# skill/
> L2 | 父级: ../CLAUDE.md
legacy.go: 按旧导出器的固定标记识别 SKILL.md、KONSEKI.md 和日期日记，只删除这些文件；目录空了才删目录，软链接本身保留。
legacy_test.go: 用 testdata/legacy 验证识别、删除边界、新技能与用户文件保留，以及软链接目录。
testdata/legacy/: 99394ed 的导出器生成的样本，作为旧文件识别基准。
[PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
