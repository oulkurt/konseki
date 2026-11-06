/*
 * [INPUT]: 依赖摘要、采集数据、技能导出服务与语言目录。
 * [OUTPUT]: 提供技能导出操作与主分页导航。
 * [POS]: 导出数据组装和时间轴、设置页之间的导航。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import (
	"fmt"
	"os"

	"konseki/internal/skill"
)

// exportSkill 收集最近 N 天的数据并写出。
func (a *app) exportSkill() (int, error) {
	if a.runner == nil || a.sums == nil {
		return 0, fmt.Errorf("还没准备好")
	}
	days := 7
	if days <= 0 {
		days = 7
	}

	// 先在 Store 里看哪些天有摘要 —— 没有摘要的日期不导出，
	// 免得给 Agent 一堆空文件。
	have := a.sums.Days()
	if len(have) > days {
		have = have[:days]
	}
	if len(have) == 0 {
		// 还没生成过摘要，就把今天的时间轴导出去。
		have = []string{a.dayKey()}
	}

	var data []skill.DayData
	for _, day := range have {
		blocks, err := a.runner.Timeline(day)
		if err != nil {
			continue
		}
		entries, _ := a.sums.Load(day)
		d := skill.DayData{Day: day, Blocks: blocks, Entries: entries}
		data = append(data, d)
	}

	opts := skill.Options{
		Dir:   a.cfgNow.Skill.ExportDir,
		Days:  days,
		Owner: ownerName(),
	}
	if opts.Dir == "" {
		opts.Dir = a.skillDefaultDirectory()
	}
	return skill.Export(opts, data)
}

func (a *app) skillDefaultDirectory() string {
	if a.defaultSkillDir != nil {
		return a.defaultSkillDir()
	}
	return skill.DefaultDir()
}

// ownerName 拿系统的用户名当默认称呼。
//
// 不引入配置项：这个值只影响 SKILL.md 里的一句话，让用户专门
// 去填一个名字收益太低。想改就直接改导出的文件。
func ownerName() string {
	if u := os.Getenv("USER"); u != "" {
		return u
	}
	return "the user"
}
