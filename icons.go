/*
 * [INPUT]: 依赖 MyGo 的 SVG 图标解析。
 * [OUTPUT]: 提供界面控件使用的矢量图标。
 * [POS]: 根模块共用图标，供设置和导航引用。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import "github.com/egoist/mygo/ui"

// UI Chrome Icons (monochrome, used with ui.Icon)
var (
	chevronsUpDownSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M5.5 6L8 3.5L10.5 6"/><path d="M5.5 10L8 12.5L10.5 10"/></svg>`))
	infoCircleSVG     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6.25"/><path d="M8 7.2V11"/><circle cx="8" cy="4.85" r="0.75" fill="currentColor" stroke="none"/></svg>`))
	moreHorizontalSVG = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><circle cx="3.5" cy="8" r="1.25"/><circle cx="8" cy="8" r="1.25"/><circle cx="12.5" cy="8" r="1.25"/></svg>`))
	chevronDownSVG    = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4.75 6.25L8 9.5L11.25 6.25"/></svg>`))
	chevronUpSVG      = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" stroke-linejoin="round"><path d="M4.75 9.5L8 6.25L11.25 9.5"/></svg>`))
	plusSVG           = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.6" stroke-linecap="round"><path d="M8 3.25V12.75M3.25 8H12.75"/></svg>`))
	modelClusterSVG   = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="currentColor"><circle cx="8" cy="3.2" r="1.45"/><circle cx="12.1" cy="5.6" r="1.45"/><circle cx="12.1" cy="10.4" r="1.45"/><circle cx="8" cy="12.8" r="1.45"/><circle cx="3.9" cy="10.4" r="1.45"/><circle cx="3.9" cy="5.6" r="1.45"/><circle cx="8" cy="8" r="1.15"/></svg>`))
	globeSVG          = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.35" stroke-linecap="round" stroke-linejoin="round"><circle cx="8" cy="8" r="6"/><path d="M2.2 8H13.8"/><path d="M8 2C9.7 3.7 10.6 5.8 10.6 8C10.6 10.2 9.7 12.3 8 14C6.3 12.3 5.4 10.2 5.4 8C5.4 5.8 6.3 3.7 8 2Z"/></svg>`))
	checkSVG          = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 8.5L6.5 11.5L12.5 4.8"/></svg>`))
	closeSmallSVG     = ui.MustParseSVG([]byte(`<svg viewBox="0 0 16 16" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M4.5 4.5L11.5 11.5M11.5 4.5L4.5 11.5"/></svg>`))
)
