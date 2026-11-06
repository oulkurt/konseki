/*
 * [INPUT]: 依赖 testdata/legacy 的旧导出样本和临时目录。
 * [OUTPUT]: 验证旧文件识别、删除边界、用户文件保留和软链接目录处理。
 * [POS]: skill 包只删旧导出文件的回归测试。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package skill

import (
	"os"
	"path/filepath"
	"testing"
)

func copyFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.CopyFS(dir, os.DirFS("testdata/legacy")); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, path, text string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestFindAndRemoveLegacy(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir)
	write(t, filepath.Join(dir, "notes.md"), "用户自己的笔记")
	write(t, filepath.Join(dir, "days", "x.md"), "不是日期命名的文件")

	files, err := FindLegacy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 4 {
		t.Fatalf("识别到 %d 个旧文件: %v", len(files), files)
	}
	n, err := RemoveLegacy(dir)
	if err != nil || n != 4 {
		t.Fatalf("删除 %d 个: %v", n, err)
	}
	if !exists(filepath.Join(dir, "notes.md")) || !exists(filepath.Join(dir, "days", "x.md")) {
		t.Fatal("删掉了用户文件")
	}
	if exists(filepath.Join(dir, "SKILL.md")) || exists(filepath.Join(dir, "KONSEKI.md")) {
		t.Fatal("旧文件残留")
	}
	if files, _ := FindLegacy(dir); len(files) != 0 {
		t.Fatalf("删除后仍识别到: %v", files)
	}
	// 再删一次没有文件可删，也不出错。
	if n, err := RemoveLegacy(dir); err != nil || n != 0 {
		t.Fatalf("重复删除: %d, %v", n, err)
	}
}

func TestRemoveKeepsNewSkill(t *testing.T) {
	dir := t.TempDir()
	copyFixture(t, dir)
	b, err := os.ReadFile(filepath.Join("..", "..", "skills", "konseki", "SKILL.md"))
	if err != nil {
		t.Fatal(err)
	}
	newSkill := string(b)
	write(t, filepath.Join(dir, "SKILL.md"), newSkill)

	files, err := FindLegacy(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 {
		t.Fatalf("新 SKILL.md 被误认: %v", files)
	}
	n, err := RemoveLegacy(dir)
	if err != nil || n != 3 {
		t.Fatalf("删除 %d 个: %v", n, err)
	}
	b, err = os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || string(b) != newSkill {
		t.Fatalf("新技能被改动: %v", err)
	}
	if exists(filepath.Join(dir, "KONSEKI.md")) || exists(filepath.Join(dir, "days")) {
		t.Fatal("旧导出目录未清理")
	}
}

func TestLegacySkillNeedsAllMarkers(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "SKILL.md"), "---\nname: konseki\n---\n\n# 痕跡 Konseki — 用户改过的\n")
	write(t, filepath.Join(dir, "KONSEKI.md"), "# 我的画像\n\n不是旧导出器写的。\n")
	if files, err := FindLegacy(dir); err != nil || len(files) != 0 {
		t.Fatalf("误认: %v %v", files, err)
	}
}

func TestRemoveLegacyThroughSymlink(t *testing.T) {
	target := t.TempDir()
	copyFixture(t, target)
	parent := t.TempDir()
	link := filepath.Join(parent, "konseki")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}

	files, err := FindLegacy(link)
	if err != nil || len(files) != 4 {
		t.Fatalf("识别: %v %v", files, err)
	}
	n, err := RemoveLegacy(link)
	if err != nil || n != 4 {
		t.Fatalf("删除 %d 个: %v", n, err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("链接被删除: %v", err)
	}
	if !exists(target) {
		t.Fatal("链接目标被删除")
	}
	if exists(filepath.Join(target, "days")) || exists(filepath.Join(target, "SKILL.md")) {
		t.Fatal("目标目录里的旧文件残留")
	}
}
