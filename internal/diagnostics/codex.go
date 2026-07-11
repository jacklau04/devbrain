package diagnostics

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var (
	codexStateTableRe = regexp.MustCompile(`^\[hooks\.state\.("(?:\\.|[^"])*")\]$`)
	codexBoolRe       = regexp.MustCompile(`^(hooks|enabled)\s*=\s*(true|false)(?:\s+#.*)?$`)
	codexHashRe       = regexp.MustCompile(`^trusted_hash\s*=\s*("(?:\\.|[^"])*")(?:\s+#.*)?$`)
)

var codexEventLabels = map[string]string{
	"PreToolUse":        "pre_tool_use",
	"PermissionRequest": "permission_request",
	"PostToolUse":       "post_tool_use",
	"PreCompact":        "pre_compact",
	"PostCompact":       "post_compact",
	"SessionStart":      "session_start",
	"UserPromptSubmit":  "user_prompt_submit",
	"SubagentStart":     "subagent_start",
	"SubagentStop":      "subagent_stop",
	"Stop":              "stop",
}

// CodexHooksReport describes whether registered devbrain hooks can actually
// execute. Codex skips new or changed user hooks until their fingerprints are
// trusted, even when hooks.json and the feature flag are otherwise correct.
type CodexHooksReport struct {
	Configured     bool              `json:"configured"`
	HooksPath      string            `json:"hooks_path"`
	ConfigPath     string            `json:"config_path"`
	FeatureEnabled bool              `json:"feature_enabled"`
	Registered     int               `json:"registered"`
	Trusted        int               `json:"trusted"`
	PendingTrust   int               `json:"pending_trust"`
	Modified       int               `json:"modified"`
	Disabled       int               `json:"disabled"`
	Hooks          []CodexHookStatus `json:"hooks"`
	Remediation    string            `json:"remediation"`
	Error          string            `json:"error"`
}

type CodexHookStatus struct {
	Event  string `json:"event"`
	Hook   string `json:"hook"`
	Status string `json:"status"`
}

type codexHooksFile struct {
	Hooks map[string][]codexMatcherGroup `json:"hooks"`
}

type codexMatcherGroup struct {
	Matcher *string        `json:"matcher"`
	Hooks   []codexHandler `json:"hooks"`
}

type codexHandler struct {
	Type           string  `json:"type"`
	Command        string  `json:"command"`
	CommandWindows *string `json:"commandWindows"`
	Timeout        *uint64 `json:"timeout"`
	Async          bool    `json:"async"`
	StatusMessage  *string `json:"statusMessage"`
}

type codexHookDefinition struct {
	event, eventLabel, hook, stateKey, currentHash string
}

type codexHookState struct {
	trustedHash string
	enabled     *bool
}

// ReportCodexHooks reads Codex hook definitions and persisted trust state. It
// uses the same normalized SHA-256 identity as Codex so changed hooks are not
// mistaken for trusted hooks just because an old trusted_hash still exists.
func ReportCodexHooks(codexHome string) CodexHooksReport {
	if codexHome == "" {
		codexHome = os.Getenv("CODEX_HOME")
	}
	if codexHome == "" {
		home, _ := os.UserHomeDir()
		codexHome = filepath.Join(home, ".codex")
	}
	r := CodexHooksReport{
		HooksPath:   filepath.Join(codexHome, "hooks.json"),
		ConfigPath:  filepath.Join(codexHome, "config.toml"),
		Hooks:       []CodexHookStatus{},
		Remediation: "Open Codex and run /hooks, review the devbrain commands, then choose Trust all and continue.",
	}
	defs, missing, err := readCodexHookDefinitions(r.HooksPath)
	if missing {
		return r
	}
	r.Configured = true
	if err != nil {
		r.Error = err.Error()
		return r
	}
	feature, states, err := readCodexConfig(r.ConfigPath)
	if err != nil {
		r.Error = err.Error()
	}
	r.FeatureEnabled = feature
	r.Registered = len(defs)
	for _, d := range defs {
		state := states[d.stateKey]
		status := "untrusted"
		switch {
		case state.enabled != nil && !*state.enabled:
			status = "disabled"
			r.Disabled++
		case state.trustedHash == "":
			r.PendingTrust++
		case state.trustedHash != d.currentHash:
			status = "modified"
			r.Modified++
		default:
			status = "trusted"
			r.Trusted++
		}
		r.Hooks = append(r.Hooks, CodexHookStatus{Event: d.event, Hook: d.hook, Status: status})
	}
	return r
}

func readCodexHookDefinitions(path string) ([]codexHookDefinition, bool, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, true, nil
		}
		return nil, false, err
	}
	var file codexHooksFile
	if err := json.Unmarshal(b, &file); err != nil {
		return nil, false, fmt.Errorf("parse %s: %w", path, err)
	}
	var defs []codexHookDefinition
	for event, groups := range file.Hooks {
		label, ok := codexEventLabels[event]
		if !ok {
			continue
		}
		for groupIndex, group := range groups {
			for handlerIndex, handler := range group.Hooks {
				hook, ok := devbrainHookName(handler.Command)
				if handler.Type != "command" || !ok {
					continue
				}
				defs = append(defs, codexHookDefinition{
					event:       event,
					eventLabel:  label,
					hook:        hook,
					stateKey:    fmt.Sprintf("%s:%s:%d:%d", path, label, groupIndex, handlerIndex),
					currentHash: codexHookHash(label, group.Matcher, handler),
				})
			}
		}
	}
	return defs, false, nil
}

func devbrainHookName(command string) (string, bool) {
	fields := strings.Fields(command)
	if len(fields) == 4 && fields[0] == "DEVBRAIN_HARNESS=codex" {
		fields = fields[1:]
	}
	if len(fields) != 3 || fields[1] != "hook" || !strings.Contains(filepath.Base(fields[0]), "devbrain") {
		return "", false
	}
	return fields[2], true
}

func codexHookHash(eventLabel string, matcher *string, handler codexHandler) string {
	timeout := uint64(600)
	if handler.Timeout != nil {
		timeout = *handler.Timeout
		if timeout < 1 {
			timeout = 1
		}
	}
	normalized := map[string]any{
		"type":    "command",
		"command": handler.Command,
		"timeout": timeout,
		"async":   handler.Async,
	}
	if handler.StatusMessage != nil {
		normalized["statusMessage"] = *handler.StatusMessage
	}
	identity := map[string]any{
		"event_name": eventLabel,
		"hooks":      []any{normalized},
	}
	// UserPromptSubmit and Stop do not support matchers; Codex removes them
	// before fingerprinting even if a source file happens to include one.
	if matcher != nil && eventLabel != "user_prompt_submit" && eventLabel != "stop" {
		identity["matcher"] = *matcher
	}
	b, _ := json.Marshal(identity) // encoding/json sorts map keys recursively.
	sum := sha256.Sum256(b)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func readCodexConfig(path string) (bool, map[string]codexHookState, error) {
	states := map[string]codexHookState{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, states, nil
		}
		return false, states, err
	}
	defer f.Close()
	section, stateKey := "", ""
	featureEnabled := false
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			section, stateKey = "", ""
			switch {
			case line == "[features]":
				section = "features"
			case codexStateTableRe.MatchString(line):
				quoted := codexStateTableRe.FindStringSubmatch(line)[1]
				if key, err := strconv.Unquote(quoted); err == nil {
					section, stateKey = "state", key
				}
			}
			continue
		}
		switch section {
		case "features":
			if m := codexBoolRe.FindStringSubmatch(line); m != nil && m[1] == "hooks" {
				featureEnabled = m[2] == "true"
			}
		case "state":
			state := states[stateKey]
			if m := codexHashRe.FindStringSubmatch(line); m != nil {
				if hash, err := strconv.Unquote(m[1]); err == nil {
					state.trustedHash = hash
				}
			} else if m := codexBoolRe.FindStringSubmatch(line); m != nil && m[1] == "enabled" {
				enabled := m[2] == "true"
				state.enabled = &enabled
			}
			states[stateKey] = state
		}
	}
	return featureEnabled, states, scanner.Err()
}
