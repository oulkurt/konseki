/*
 * [INPUT]: 依赖 presets.go 的初始化赋值。
 * [OUTPUT]: 持有供目录查询使用的 presets 切片。
 * [POS]: 公开目录 API 与生成数据之间的存储声明。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package providers

// presets 是预设表本体，由 presets.go 的 init 填充。
//
// 用 var + init 而不是直接写字面量：那张表 700 多行，
// 直接放在这个文件里会把上面的 API 部分埋掉。
var presets []Preset
