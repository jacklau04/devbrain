package diagnostics

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const codexHooksFixture = `{
  "hooks": {
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "DEVBRAIN_HARNESS=codex /opt/homebrew/bin/devbrain hook capture"}]}],
    "PostToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "DEVBRAIN_HARNESS=codex /opt/homebrew/bin/devbrain hook gbrain"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "DEVBRAIN_HARNESS=codex /opt/homebrew/bin/devbrain hook response"}]}],
    "SessionStart": [{"matcher": "startup|resume", "hooks": [{"type": "command", "command": "DEVBRAIN_HARNESS=codex /opt/homebrew/bin/devbrain hook session-start"}]}]
  }
}`

func TestCodexHookHashMatchesCodexFingerprint(t *testing.T) {
	got := codexHookHash("user_prompt_submit", nil, codexHandler{
		Type:    "command",
		Command: "DEVBRAIN_HARNESS=codex /opt/homebrew/bin/devbrain hook capture",
	})
	const want = "sha256:a068aff826b460ede0a64795493566f96c9a19b2e4cae443aee29c6c0eed07fe"
	if got != want {
		t.Fatalf("Codex-compatible hook fingerprint = %q, want %q", got, want)
	}
}

func TestReportCodexHooksTrustStates(t *testing.T) {
	home := t.TempDir()
	hooksPath := filepath.Join(home, "hooks.json")
	writeFile(t, hooksPath, codexHooksFixture)
	writeFile(t, filepath.Join(home, "config.toml"), "[features]\nhooks = true\n")

	pending := ReportCodexHooks(home)
	if !pending.Configured || !pending.FeatureEnabled || pending.Registered != 4 || pending.PendingTrust != 4 {
		t.Fatalf("pending report = %+v", pending)
	}

	defs, missing, err := readCodexHookDefinitions(hooksPath)
	if err != nil || missing {
		t.Fatalf("read hook definitions: missing=%v err=%v", missing, err)
	}
	var config strings.Builder
	config.WriteString("[features]\nhooks = true\n\n[hooks.state]\n")
	for _, d := range defs {
		fmt.Fprintf(&config, "\n[hooks.state.%s]\ntrusted_hash = %s\n", strconv.Quote(d.stateKey), strconv.Quote(d.currentHash))
	}
	writeFile(t, filepath.Join(home, "config.toml"), config.String())

	trusted := ReportCodexHooks(home)
	if trusted.Trusted != 4 || trusted.PendingTrust != 0 || trusted.Modified != 0 || trusted.Disabled != 0 {
		t.Fatalf("trusted report = %+v", trusted)
	}

	changed := strings.Replace(codexHooksFixture, "/opt/homebrew/bin/devbrain hook capture", "/usr/local/bin/devbrain hook capture", 1)
	writeFile(t, hooksPath, changed)
	modified := ReportCodexHooks(home)
	if modified.Modified != 1 || modified.Trusted != 3 {
		t.Fatalf("modified report = %+v", modified)
	}
}

func TestReportCodexHooksFeatureAndDisabledState(t *testing.T) {
	home := t.TempDir()
	hooksPath := filepath.Join(home, "hooks.json")
	writeFile(t, hooksPath, codexHooksFixture)
	defs, _, err := readCodexHookDefinitions(hooksPath)
	if err != nil {
		t.Fatal(err)
	}
	d := defs[0]
	config := "[features]\nhooks = false\n\n[hooks.state." + strconv.Quote(d.stateKey) + "]\n" +
		"trusted_hash = " + strconv.Quote(d.currentHash) + "\nenabled = false\n"
	writeFile(t, filepath.Join(home, "config.toml"), config)

	r := ReportCodexHooks(home)
	if r.FeatureEnabled || r.Disabled != 1 || r.PendingTrust != 3 {
		t.Fatalf("disabled report = %+v", r)
	}
}

func TestReportCodexHooksMissingIsNotConfigured(t *testing.T) {
	r := ReportCodexHooks(t.TempDir())
	if r.Configured || r.Registered != 0 || r.Error != "" {
		t.Fatalf("missing hooks report = %+v", r)
	}
}

func TestReadCodexConfigRejectsUnreadableFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.toml")
	if err := os.Mkdir(path, 0o755); err != nil {
		t.Fatal(err)
	}
	_, _, err := readCodexConfig(path)
	if err == nil {
		t.Fatal("directory config path should fail")
	}
}
