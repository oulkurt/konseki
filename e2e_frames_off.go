//go:build !e2e

/*
 * [INPUT]: 无。
 * [OUTPUT]: 提供 probeShredFrame 与 probeShredDone 的空实现。
 * [POS]: 正式构建不记帧耗时；测试构建换成 e2e_frames.go。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package main

import "time"

func probeShredFrame(time.Time, bool) {}
func probeShredDone()                 {}
