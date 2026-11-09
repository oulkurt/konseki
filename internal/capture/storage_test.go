/*
 * [INPUT]: 依赖临时目录、合成 JPEG 和存储维护接口。
 * [OUTPUT]: 验证统计、配额、降级、清空、并发与性能。
 * [POS]: 以合成数据验证存储策略，不启动采集循环。
 * [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
 */
package capture

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func writeTestFile(t *testing.T, root, name string, data []byte) {
	t.Helper()
	p := filepath.Join(root, name)
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0644); err != nil {
		t.Fatal(err)
	}
}
func testJPEG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 2560, 1600))
	for y := 0; y < 1600; y++ {
		for x := 0; x < 2560; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(x), uint8(y), uint8(x + y), 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
func TestStorageOperations(t *testing.T) {
	root := t.TempDir()
	s := NewStorage(root)
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	data := testJPEG(t)
	names := []string{"shots/20261026-120000.jpg", "shots/2026-11-03/120000.000-abcd.jpg", "shots/2026-11-05/120000.000-abcd.jpg"}
	for _, n := range names {
		writeTestFile(t, root, n, data)
	}
	writeTestFile(t, root, "2026-11-05.jsonl", []byte("{}\n{}\n"))
	writeTestFile(t, root, "sessions/2026-11-05.jsonl", []byte("{}\n"))
	writeTestFile(t, root, "config.json", []byte("keep"))
	writeTestFile(t, root, "summaries/2026-11-04.jsonl", []byte("keep"))
	if err := s.Rescan(); err != nil {
		t.Fatal(err)
	}
	if u := s.Usage(); u.Shots != 3 || u.Snapshots != 2 || u.Sessions != 1 || u.Bytes != int64(3*len(data)) {
		t.Fatal(u)
	}
	n, err := s.Degrade(now)
	if err != nil || n != 2 {
		t.Fatalf("degrade %d %v", n, err)
	}
	n, err = s.Degrade(now)
	if err != nil || n != 0 {
		t.Fatalf("repeat %d %v", n, err)
	}
	for i, limits := range [][2]int{{640, 360}, {1280, 720}} {
		f, err := os.Open(filepath.Join(root, names[i]))
		if err != nil {
			t.Fatal(err)
		}
		c, err := jpeg.DecodeConfig(f)
		f.Close()
		if err != nil || c.Width > limits[0] || c.Height > limits[1] {
			t.Fatal(c, err)
		}
	}
	before := s.Usage()
	if _, err = s.Enforce(before.Bytes / 2); err != nil {
		t.Fatal(err)
	}
	if s.Usage().Bytes > before.Bytes/2 {
		t.Fatal(s.Usage())
	}
	if _, err = os.Stat(filepath.Join(root, names[0])); !os.IsNotExist(err) {
		t.Fatal(err)
	}
	cached := s.Usage()
	if err = s.Rescan(); err != nil || cached != s.Usage() {
		t.Fatal(cached, s.Usage(), err)
	}
	cleared, err := s.ClearAll()
	if err != nil || cleared.Sessions != 1 {
		t.Fatal(cleared, err)
	}
	again, err := s.ClearAll()
	if err != nil || again != (Cleared{}) {
		t.Fatal(again, err)
	}
	for _, n := range []string{"config.json", "summaries/2026-11-04.jsonl"} {
		if _, err = os.Stat(filepath.Join(root, n)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestStoragePerformance(t *testing.T) {
	if os.Getenv("KONSEKI_STORAGE_PERF") != "1" {
		t.Skip("手动运行性能测试")
	}
	root := t.TempDir()
	s := NewStorage(root)
	data := testJPEG(t)
	for i := 0; i < 300; i++ {
		writeTestFile(t, root, filepath.Join("shots", "2026-01-01", time.Date(2026, 1, 1, 0, 0, i, 0, time.Local).Format("150405.000")+"-abcd.jpg"), data)
	}
	if err := s.Rescan(); err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	var start, peak runtime.MemStats
	runtime.ReadMemStats(&start)
	stop := make(chan struct{})
	done := make(chan uint64)
	go func() {
		var max uint64
		for {
			select {
			case <-stop:
				done <- max
				return
			default:
				runtime.ReadMemStats(&peak)
				if peak.Alloc > max {
					max = peak.Alloc
				}
				time.Sleep(time.Millisecond)
			}
		}
	}()
	begin := time.Now()
	n, err := s.Degrade(time.Date(2026, 1, 3, 0, 0, 0, 0, time.Local))
	elapsed := time.Since(begin)
	close(stop)
	max := <-done
	if err != nil || n != 300 {
		t.Fatal(n, err)
	}
	t.Logf("平均 %v，峰值堆内存 %.1f MiB，增量 %.1f MiB，截图字节 %d → %d", elapsed/300, float64(max)/(1024*1024), float64(max-start.Alloc)/(1024*1024), 300*len(data), s.Usage().Bytes)
	if elapsed/300 > 50*time.Millisecond || max > 200*1024*1024 {
		t.Fatalf("性能超出限制：平均 %v，峰值 %.1f MiB", elapsed/300, float64(max)/(1024*1024))
	}
}

func TestStorageConcurrent(t *testing.T) {
	root := t.TempDir()
	s := NewStorage(root)
	data := testJPEG(t)
	dir := filepath.Join(root, "shots", "2026-11-05")
	if err := os.MkdirAll(dir, 0755); err != nil {
		t.Fatal(err)
	}
	first := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan error, 1)
	go func() {
		for i := 0; i < 60; i++ {
			tmp, err := os.CreateTemp(dir, ".capture-*.tmp")
			if err != nil {
				done <- err
				return
			}
			if _, err = tmp.Write(data); err != nil {
				tmp.Close()
				done <- err
				return
			}
			if err = tmp.Close(); err != nil {
				done <- err
				return
			}
			name := fmt.Sprintf("1200%02d.000-abcd.jpg", i)
			if err = os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
				done <- err
				return
			}
			s.Account(int64(len(data)))
			if i == 0 {
				close(first)
				<-resume
			}
			time.Sleep(time.Millisecond)
		}
		done <- nil
	}()
	select {
	case <-first:
	case err := <-done:
		t.Fatalf("first write: %v", err)
	}
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.Local)
	degraded, err := s.Degrade(now)
	if err != nil || degraded != 1 {
		close(resume)
		t.Fatalf("first degrade: %d %v", degraded, err)
	}
	close(resume)
	var freed int64
	for {
		bytes, err := s.Enforce(int64(len(data)) * 20)
		if err != nil {
			t.Fatal(err)
		}
		freed += bytes
		n, err := s.Degrade(now)
		if err != nil {
			t.Fatal(err)
		}
		degraded += n
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
			bytes, err = s.Enforce(int64(len(data)) * 20)
			if err != nil {
				t.Fatal(err)
			}
			freed += bytes
			if freed == 0 || degraded == 0 {
				t.Fatalf("maintenance did not run: %d %d", freed, degraded)
			}
			cached := s.Usage()
			fresh := NewStorage(root)
			if err := fresh.Rescan(); err != nil {
				t.Fatal(err)
			}
			disk := fresh.Usage()
			if diff := cached.Bytes - disk.Bytes; diff < -2*int64(len(data)) || diff > 2*int64(len(data)) {
				t.Fatalf("bytes: cache %+v disk %+v", cached, disk)
			}
			if diff := cached.Shots - disk.Shots; diff < -2 || diff > 2 {
				t.Fatalf("shots: cache %+v disk %+v", cached, disk)
			}
			return
		default:
		}
	}
}

func TestStorageQuotaAndCounters(t *testing.T) {
	root := t.TempDir()
	s := NewStorage(root)
	names := []string{"20261101-000000.jpg", "20261102-000000.jpg", "20261103-000000.jpg"}
	for _, name := range names {
		writeTestFile(t, root, "shots/"+name, []byte("1234567890"))
		s.Account(10)
	}
	s.CountSession()
	if u := s.Usage(); u.Bytes != 30 || u.Shots != 3 || u.Sessions != 1 {
		t.Fatal(u)
	}
	writeTestFile(t, root, "sessions/2026-11-05.jsonl", []byte("{}\n"))
	writeTestFile(t, root, "2026-11-05.jsonl", []byte("{}\n"))
	if err := s.Rescan(); err != nil {
		t.Fatal(err)
	}
	if freed, err := s.Enforce(30); err != nil || freed != 0 {
		t.Fatal(freed, err)
	}
	if freed, err := s.Enforce(25); err != nil || freed != 10 {
		t.Fatal(freed, err)
	}
	if freed, err := s.Enforce(10); err != nil || freed != 20 {
		t.Fatal(freed, err)
	}
	if _, err := os.Stat(filepath.Join(root, "2026-11-05.jsonl")); err != nil {
		t.Fatal(err)
	}
	before := s.Usage()
	if err := s.Rescan(); err != nil || before != s.Usage() {
		t.Fatal(before, s.Usage(), err)
	}
}

func TestDegradeSkipsFullDecodeAndPreservesRecent(t *testing.T) {
	root := t.TempDir()
	now := time.Date(2026, 11, 5, 12, 0, 0, 0, time.Local)
	names := []string{"shots/2026-11-03/120000.000-abcd.jpg", "shots/20261026-120000.jpg", "shots/2026-11-05/110000.000-abcd.jpg"}
	data := testJPEG(t)
	for _, name := range names {
		writeTestFile(t, root, name, data)
	}
	s := NewStorage(root)
	if err := s.Rescan(); err != nil {
		t.Fatal(err)
	}
	if n, err := s.Degrade(now); err != nil || n != 2 {
		t.Fatalf("degrade: %d %v", n, err)
	}
	var hashes [3][32]byte
	for i, name := range names {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil {
			t.Fatal(err)
		}
		hashes[i] = sha256.Sum256(content)
		if i == 2 {
			if !bytes.Equal(content, data) {
				t.Fatal("recent screenshot changed")
			}
			continue
		}
		cfg, err := jpeg.DecodeConfig(bytes.NewReader(content))
		limits := [][2]int{{1280, 720}, {640, 360}}
		if err != nil || cfg.Width > limits[i][0] || cfg.Height > limits[i][1] {
			t.Fatalf("%s: %v %v", name, cfg, err)
		}
	}
	before := fullDecodeCount.Load()
	if n, err := s.Degrade(now); err != nil || n != 0 {
		t.Fatalf("repeat: %d %v", n, err)
	}
	if got := fullDecodeCount.Load(); got != before {
		t.Fatalf("decoded again: %d", got-before)
	}
	for i, name := range names {
		content, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || sha256.Sum256(content) != hashes[i] {
			t.Fatalf("changed %s: %v", name, err)
		}
	}
}

func TestClearAllSentinelsAndInterruptedCleanup(t *testing.T) {
	root := t.TempDir()
	sentinels := []string{"config.json", "config.json.tmp", "secrets.json", "secrets.json.tmp", "summaries/2026-11-04.jsonl", "unknown.txt"}
	for _, name := range sentinels {
		writeTestFile(t, root, name, []byte("keep"))
	}
	for _, name := range []string{"shots/20261101-000000.jpg", "shots/20261102-000000.jpg", "shots/2026-11-03/000000.000-abcd.jpg", "shots/2026-11-04/000000.000-abcd.jpg"} {
		writeTestFile(t, root, name, []byte("12345"))
	}
	writeTestFile(t, root, "2026-11-01.jsonl", []byte("{}\n"))
	writeTestFile(t, root, "2026-11-02.jsonl", []byte("{}\n"))
	writeTestFile(t, root, "sessions/2026-11-01.jsonl", []byte("{}\n{}\n"))
	writeTestFile(t, root, "ocr/2026-11-01.jsonl", []byte("{}\n"))
	for _, name := range []string{"shots/20261101-000000.jpg", "shots/20261102-000000.jpg", "2026-11-01.jsonl"} {
		if err := os.Remove(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	s := NewStorage(root)
	got, err := s.ClearAll()
	if err != nil || got != (Cleared{Shots: 2, Freed: 10, Sessions: 2}) {
		t.Fatalf("clear: %+v %v", got, err)
	}
	got, err = s.ClearAll()
	if err != nil || got != (Cleared{}) {
		t.Fatalf("repeat: %+v %v", got, err)
	}
	for _, name := range sentinels {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(b) != "keep" {
			t.Fatalf("sentinel %s: %q %v", name, b, err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "2026-11-02.jsonl")); !os.IsNotExist(err) {
		t.Fatalf("day remains: %v", err)
	}
}

func TestClearAllPartialFailureCountsActualRemoval(t *testing.T) {
	root := t.TempDir()
	blocked := filepath.Join(root, "shots", "2026-11-01")
	writeTestFile(t, root, "shots/2026-11-01/000000.000-abcd.jpg", []byte("1234567"))
	writeTestFile(t, root, "shots/2026-11-02/000000.000-abcd.jpg", []byte("12345"))
	probe := filepath.Join(blocked, "probe")
	if err := os.WriteFile(probe, nil, 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(blocked, 0700) })
	if err := os.Remove(probe); err == nil {
		t.Skip("当前用户可绕过目录权限")
	} else if !os.IsPermission(err) {
		t.Fatal(err)
	}
	s := NewStorage(root)
	got, err := s.ClearAll()
	if err != nil || got.Failed == 0 || got.Shots != 1 || got.Freed != 5 {
		t.Fatalf("partial: %+v %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(blocked, "000000.000-abcd.jpg")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(blocked, 0700); err != nil {
		t.Fatal(err)
	}
	got, err = s.ClearAll()
	if err != nil || got != (Cleared{Shots: 1, Freed: 7}) {
		t.Fatalf("retry: %+v %v", got, err)
	}
}

func TestEnforceUsesDiskTotalAndPreservesSnapshots(t *testing.T) {
	root := t.TempDir()
	names := []string{"shots/20261101-000000.jpg", "shots/20261102-000000.jpg", "shots/20261103-000000.jpg"}
	for _, name := range names {
		writeTestFile(t, root, name, []byte("1234567890"))
	}
	day := []byte("{\"shot\":\"retained\"}\n")
	writeTestFile(t, root, "2026-11-01.jsonl", day)
	s := NewStorage(root)
	if err := s.Rescan(); err != nil {
		t.Fatal(err)
	}
	if freed, err := s.Enforce(30); err != nil || freed != 0 {
		t.Fatalf("initial: %d %v", freed, err)
	}
	// 模拟缓存高于磁盘；删除目标必须由目录快照决定。
	s.mu.Lock()
	s.usage.Bytes = 100
	s.mu.Unlock()
	if freed, err := s.Enforce(25); err != nil || freed != 10 {
		t.Fatalf("lower limit: %d %v", freed, err)
	}
	if _, err := os.Stat(filepath.Join(root, names[0])); !os.IsNotExist(err) {
		t.Fatalf("oldest remains: %v", err)
	}
	for _, name := range names[1:] {
		if _, err := os.Stat(filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}
	}
	if u := s.Usage(); u.Bytes != 90 || u.Shots != 2 {
		t.Fatalf("cache: %+v", u)
	}
	got, err := os.ReadFile(filepath.Join(root, "2026-11-01.jsonl"))
	if err != nil || !bytes.Equal(got, day) {
		t.Fatalf("snapshot changed: %q %v", got, err)
	}
}

func TestStorageIgnoresTemporaryFiles(t *testing.T) {
	root := t.TempDir()
	files := []string{"shots/2026-11-01/.000000.000-abcd.jpg", "shots/2026-11-01/000000.000-abcd.jpg.tmp", "shots/2026-11-01/.degrade-123.tmp"}
	for _, name := range files {
		writeTestFile(t, root, name, []byte("temporary"))
	}
	s := NewStorage(root)
	if err := s.Rescan(); err != nil {
		t.Fatal(err)
	}
	if u := s.Usage(); u.Shots != 0 || u.Bytes != 0 {
		t.Fatal(u)
	}
	if n, err := s.Degrade(time.Date(2026, 11, 15, 0, 0, 0, 0, time.Local)); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	if n, err := s.Enforce(-1); err != nil || n != 0 {
		t.Fatal(n, err)
	}
	for _, name := range files {
		b, err := os.ReadFile(filepath.Join(root, name))
		if err != nil || string(b) != "temporary" {
			t.Fatalf("%s: %q %v", name, b, err)
		}
	}
}
