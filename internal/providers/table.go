/*
 * [INPUT]: 依赖 presets.go 的初始化赋值。
 * [OUTPUT]: 持有供目录查询使用的 presets 切片，并追加 Konseki 自有预设。
 * [POS]: 公开目录 API 与生成数据之间的存储声明。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package providers

// presets 是预设表本体，由 presets.go 的 init 填充。
//
// 用 var + init 而不是直接写字面量：那张表 700 多行，
// 直接放在这个文件里会把上面的 API 部分埋掉。
var presets []Preset

// gemini-cli 是 Konseki 自有预设，不来自 magpie。
// 它代表本机 Gemini CLI 的订阅凭据：端点由 llm.Resolve 写死，
// 所以这里不给地址；NoKey 让界面不要求填 API key。
// 放这个文件而不是 presets.go：后者由 sync-presets.py 覆盖。
//
// 依赖文件名顺序：presets.go 的 init 先跑，这里再追加。
func init() {
	presets = append(presets, Preset{
		ID:      "gemini-cli",
		Name:    "Gemini CLI",
		Kind:    KindVendor,
		Note:    "Google AI Pro/Ultra 订阅",
		Website: "https://gemini.google",
		NoKey:   true,
	})
}
