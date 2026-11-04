package providers

// presets 是预设表本体，由 presets.go 的 init 填充。
//
// 用 var + init 而不是直接写字面量：那张表 700 多行，
// 直接放在这个文件里会把上面的 API 部分埋掉。
var presets []Preset
