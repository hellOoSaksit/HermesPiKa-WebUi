// SPDX-License-Identifier: MIT AND LicenseRef-Commons-Clause-1.0

package serve

import (
	"os"
	"path/filepath"
	"testing"
)

func TestProjectFileStaysInside(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, "renders", "tex"), 0o755)
	os.MkdirAll(filepath.Join(root, ".git"), 0o755)
	for _, f := range []string{"renders/car.fbx", "renders/tex/paint.png", ".env", ".git/config", "deploy.pem", "id_ed25519"} {
		os.WriteFile(filepath.Join(root, f), []byte("x"), 0o644)
	}
	os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o644)
	_ = os.Symlink(outside, filepath.Join(root, "link")) // may fail on Windows without rights

	for _, ok := range []string{"renders/car.fbx", "renders/tex/paint.png", "/renders/car.fbx", `renders\tex\paint.png`} {
		if _, _, err := projectFile(root, ok); err != nil {
			t.Errorf("%s: %v", ok, err)
		}
	}
	for _, bad := range []string{"", ".env", ".git/config", "../" + filepath.Base(outside) + "/secret.txt", "renders/../../x", "deploy.pem", "id_ed25519", "link/secret.txt", "renders", filepath.Join(outside, "secret.txt")} {
		if _, _, err := projectFile(root, bad); err == nil {
			t.Errorf("served %q", bad)
		}
	}
}

func TestProjectFileLinksAndWindowsForms(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "docs"), 0o755)
	os.WriteFile(filepath.Join(root, ".env"), []byte("K=v"), 0o644)
	os.WriteFile(filepath.Join(root, "prod.env"), []byte("K=v"), 0o644)
	os.WriteFile(filepath.Join(root, "main.tfstate"), []byte("{}"), 0o644)
	os.WriteFile(filepath.Join(root, "ok.txt"), []byte("x"), 0o644)
	linked := os.Symlink(filepath.Join(root, ".env"), filepath.Join(root, "docs", "data.csv")) == nil
	for _, bad := range []string{"prod.env", "main.tfstate", "ok.txt.", "ok.txt ", "GIT~1/config", "ok.txt::$DATA", ".npmrc", "service-account-prod.json"} {
		if _, _, err := projectFile(root, bad); err == nil {
			t.Errorf("served %q", bad)
		}
	}
	if linked {
		if _, _, err := projectFile(root, "docs/data.csv"); err == nil {
			t.Error("a link to .env is not served")
		}
	}
	if _, _, err := projectFile(root, "ok.txt"); err != nil {
		t.Errorf("plain file: %v", err)
	}
}
