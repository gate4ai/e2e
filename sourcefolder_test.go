// SPDX-License-Identifier: AGPL-3.0-only

package e2e

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Разработка коннектора: установка из папки с исходниками (daemon собирает
// сам), update пересобирает из той же папки, --watch — на каждое изменение.
func TestConnectorFromSourceFolder(t *testing.T) {
	g := startGate(t)
	src := filepath.Join(t.TempDir(), "mail-src")
	if err := os.CopyFS(src, os.DirFS(filepath.Join(src0(t), "connectors", "mail"))); err != nil {
		t.Fatal(err)
	}
	os.RemoveAll(filepath.Join(src, ".git"))
	// копия вне go.work: собирается против опубликованного sdk-go из своего go.mod
	g.env = append(g.env, "GOWORK=off")

	setVersion := func(v string) {
		m := filepath.Join(src, "manifest.yaml")
		b, _ := os.ReadFile(m)
		lines := strings.Split(string(b), "\n")
		for i, l := range lines {
			if strings.HasPrefix(l, "version:") {
				lines[i] = `version: "` + v + `"`
			}
		}
		os.WriteFile(m, []byte(strings.Join(lines, "\n")), 0o644)
	}
	version := func() string {
		r := g.run("", "", "connectors", "list", "--json")
		for _, c := range r.json(t)["connectors"].([]any) {
			if c := c.(map[string]any); c["connector"] == "email" {
				return c["version"].(string)
			}
		}
		return ""
	}

	r := g.run("", "", "connectors", "install", src, "--yes")
	r.out = r.out[strings.Index(r.out, "{\n"):] // перед JSON — возможности для согласия (stderr)
	res := r.json(t)
	inst, _ := res["installed"].(map[string]any)
	if r.code != 0 || inst["source"] != "dir:"+src || inst["trust"] != "local" {
		t.Fatalf("install from folder: %d %s", r.code, r.out)
	}

	setVersion("dev1")
	if r := g.run("", "", "connectors", "update", "email"); r.code != 0 || version() != "dev1" {
		t.Fatalf("update from folder: %d %s (version %q)", r.code, r.out, version())
	}

	w := exec.Command(g.bin, "connectors", "update", "email", "--watch", "--yes")
	w.Env = g.env
	wout := &lockedBuf{}
	w.Stdout, w.Stderr = wout, wout
	if err := w.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { w.Process.Signal(os.Interrupt); w.Wait() }()
	for start := time.Now(); !strings.Contains(wout.String(), "watching"); time.Sleep(100 * time.Millisecond) {
		if time.Since(start) > time.Minute {
			t.Fatalf("--watch did not start:\n%s", wout.String())
		}
	}
	setVersion("dev2")
	deadline := time.Now().Add(2 * time.Minute)
	for version() != "dev2" {
		if time.Now().After(deadline) {
			t.Fatalf("--watch did not rebuild:\n%s", wout.String())
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// lockedBuf — вывод фонового процесса, который читает тест.
type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}
func (l *lockedBuf) String() string { l.mu.Lock(); defer l.mu.Unlock(); return l.b.String() }

// src0 — общий каталог соседних репозиториев (родитель e2e).
func src0(t *testing.T) string {
	return filepath.Dir(src(t, "gate4ai"))
}
