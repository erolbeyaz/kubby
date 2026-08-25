// Package kubectlsh runs a terminal that is scoped to kubectl and to one cluster.
//
// A browser cannot start a process on the reader's own machine (ADR-012), so the nearest
// useful thing is a terminal here with kubectl already pointed at the cluster they are
// looking at. What it deliberately is not is a shell: a shell on this machine would carry
// Kubby's own environment — the encryption key, the database, every stored kubeconfig —
// and would make the cluster grants, the read-only locks and the audit trail decorative.
//
// So only kubectl runs, against a kubeconfig holding one cluster, with the reader's
// impersonated identity fixed in the file rather than in an argument, and with the same
// write gate every other action passes through.
package kubectlsh

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Permission is what the reader may do, decided by the caller exactly as it is for any
// other write: the deployment kill switch, the role, and the cluster's own lock.
type Permission struct {
	MayWrite bool
	// DeniedReason is shown when a mutating command is refused, so the reader learns
	// which gate stopped them rather than guessing.
	DeniedReason string
}

// Options open a session.
type Options struct {
	Kubeconfig  []byte
	ContextName string
	ClusterName string
	Permission  Permission
	// Timeout bounds a single command. A `kubectl logs -f` is the reason this is generous
	// rather than short; a session that is closed cancels whatever is running anyway.
	Timeout time.Duration
	// OnCommand records what was run. Every command reaches it, refused ones included.
	OnCommand func(command string, allowed bool)
}

// Session is one terminal.
type Session struct {
	opts    Options
	workDir string
	// fileDir is where uploads land and where commands run. It sits a level below
	// workDir so an uploaded file can never be called `config` and take the place of
	// the kubeconfig beside it.
	fileDir  string
	uploaded int64
	out      io.Writer

	mu      sync.Mutex
	line    []rune
	history []string
	// browsing is where the reader is in the history, counted back from the end.
	browsing int
	running  context.CancelFunc
	busy     bool
	// cols is passed to kubectl as COLUMNS so its tables wrap where the reader can see
	// the wrap, rather than at a width nothing here knows about.
	cols uint16
}

// Open prepares the kubeconfig and prints the banner.
func Open(opts Options, out io.Writer) (*Session, error) {
	if _, err := exec.LookPath(kubectlBinary); err != nil {
		return nil, fmt.Errorf("kubectl is not installed alongside Kubby, so this terminal has nothing to run")
	}

	workDir, err := os.MkdirTemp("", "kubby-terminal-")
	if err != nil {
		return nil, fmt.Errorf("prepare the session: %w", err)
	}
	if err := os.Chmod(workDir, 0o700); err != nil {
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("prepare the session: %w", err)
	}
	// The credential goes in a file, never in an argument, where the process list would
	// show it to anything else running on this machine.
	if err := os.WriteFile(filepath.Join(workDir, "config"), opts.Kubeconfig, 0o600); err != nil {
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("prepare the session: %w", err)
	}

	fileDir := filepath.Join(workDir, "files")
	if err := os.MkdirAll(fileDir, 0o700); err != nil {
		_ = os.RemoveAll(workDir)
		return nil, fmt.Errorf("prepare the session: %w", err)
	}

	if opts.Timeout <= 0 {
		opts.Timeout = 30 * time.Minute
	}

	s := &Session{opts: opts, workDir: workDir, fileDir: fileDir, out: out}
	s.banner()
	s.prompt()
	return s, nil
}

// Close ends anything running and removes the credential.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.running != nil {
		s.running()
	}
	s.mu.Unlock()

	return os.RemoveAll(s.workDir)
}

// Resize is accepted and ignored: nothing here draws a full-screen interface, and kubectl
// formats to the width it is told by COLUMNS, which is set per command.
func (s *Session) Resize(cols, rows uint16) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cols > 0 {
		s.cols = cols
	}
	_ = rows
}

// Input takes what the reader typed. The line editing lives here rather than in the
// browser so the prompt, the history and the echo are one thing rather than three that
// can disagree.
func (s *Session) Input(ctx context.Context, data string) {
	for _, r := range data {
		s.key(ctx, r)
	}
}

func (s *Session) key(ctx context.Context, r rune) {
	s.mu.Lock()

	switch r {
	case '\r', '\n':
		line := strings.TrimSpace(string(s.line))
		s.line = nil
		s.browsing = 0
		s.mu.Unlock()

		s.write("\r\n")
		s.run(ctx, line)
		return

	case '\x03': // Ctrl+C
		if s.busy && s.running != nil {
			s.running()
			s.mu.Unlock()
			return
		}
		s.line = nil
		s.mu.Unlock()
		s.write("^C\r\n")
		s.prompt()
		return

	case '\x7f', '\b':
		if len(s.line) > 0 {
			s.line = s.line[:len(s.line)-1]
			s.mu.Unlock()
			// Back over the character, overwrite it with a space, back again: the only
			// way to erase on a terminal that has no idea what is behind the cursor.
			s.write("\b \b")
			return
		}
		s.mu.Unlock()
		return

	case '\x0c': // Ctrl+L
		s.mu.Unlock()
		s.write("\x1b[2J\x1b[H")
		s.prompt()
		s.write(string(s.line))
		return
	}

	if r < ' ' {
		s.mu.Unlock()
		return
	}

	s.line = append(s.line, r)
	s.mu.Unlock()
	s.write(string(r))
}

// Escape handles the arrow keys, which arrive as a sequence rather than a character.
func (s *Session) Escape(sequence string) {
	switch sequence {
	case "\x1b[A":
		s.recall(1)
	case "\x1b[B":
		s.recall(-1)
	}
}

func (s *Session) recall(direction int) {
	s.mu.Lock()

	next := s.browsing + direction
	if next < 0 || next > len(s.history) {
		s.mu.Unlock()
		return
	}
	s.browsing = next

	line := ""
	if next > 0 {
		line = s.history[len(s.history)-next]
	}
	s.line = []rune(line)
	s.mu.Unlock()

	// Erase the whole row before writing what replaces it, or the tail of a longer
	// command is left hanging after a shorter one.
	s.write("\r\x1b[K")
	s.prompt()
	s.write(line)
}

func (s *Session) run(ctx context.Context, line string) {
	if line == "" {
		s.prompt()
		return
	}

	s.mu.Lock()
	s.history = append(s.history, line)
	s.mu.Unlock()

	switch line {
	case "exit", "quit":
		s.write("\x1b[2mUse the tab's × to close this terminal.\x1b[0m\r\n")
		s.prompt()
		return
	case "clear":
		s.write("\x1b[2J\x1b[H")
		s.prompt()
		return
	case "help":
		s.help()
		s.prompt()
		return
	}

	args, err := split(line)
	if err != nil {
		s.refuse(line, err.Error())
		return
	}

	if err := s.allow(args); err != nil {
		s.refuse(line, err.Error())
		return
	}
	s.record(line, true)
	s.execute(ctx, args[0], args[1:])
}

// allow is the gate. It runs before anything is started, and it is the only place that
// decides — a check made after a process is running is not a check.
func (s *Session) allow(args []string) error {
	tool := args[0]
	if !allowedTools[tool] {
		return fmt.Errorf("only kubectl and helm run here — this terminal is scoped to the cluster, not to the host")
	}

	for _, word := range commandWords(args[1:]) {
		if reason, refused := refusedVerbs[tool][word]; refused {
			return fmt.Errorf("%s %s is not available here: %s", tool, word, reason)
		}
		if mutatingVerbs[tool][word] && !s.opts.Permission.MayWrite {
			reason := s.opts.Permission.DeniedReason
			if reason == "" {
				reason = "you may not write to this cluster"
			}
			return fmt.Errorf("%s %s is a write: %s", tool, word, reason)
		}
	}
	return nil
}

// commandWords returns every word that could be the verb.
//
// Taking the first non-flag argument is wrong and was: in `kubectl -n payments delete pod
// x` that is `payments`, the value of -n, and the delete goes through the write gate
// unnoticed. Rather than track which flags take a value — a list that drifts with every
// kubectl release — every word before `--` is considered. A namespace genuinely called
// "delete" is then treated as a write, which is the direction this should fail in.
func commandWords(args []string) []string {
	words := make([]string, 0, len(args))
	for _, arg := range args {
		if arg == "--" {
			break
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		words = append(words, arg)
	}
	return words
}

func (s *Session) execute(ctx context.Context, tool string, args []string) {
	runCtx, cancel := context.WithTimeout(ctx, s.opts.Timeout)

	s.mu.Lock()
	s.running = cancel
	s.busy = true
	cols := s.cols
	s.mu.Unlock()

	defer func() {
		cancel()
		s.mu.Lock()
		s.running = nil
		s.busy = false
		s.mu.Unlock()
		s.prompt()
	}()

	// An argument slice, never a string handed to a shell: there is no shell in this
	// path and nothing here is ever interpreted as one.
	command := exec.CommandContext(runCtx, tool, args...)
	command.Dir = s.fileDir
	command.Env = []string{
		"KUBECONFIG=" + filepath.Join(s.workDir, "config"),
		"HOME=" + s.workDir,
		"PATH=" + os.Getenv("PATH"),
		"TERM=dumb",
		fmt.Sprintf("COLUMNS=%d", orDefault(cols, 120)),
	}
	if proxy := os.Getenv("HTTPS_PROXY"); proxy != "" {
		command.Env = append(command.Env, "HTTPS_PROXY="+proxy, "NO_PROXY="+os.Getenv("NO_PROXY"))
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		s.write(red("could not start " + tool + ": " + err.Error()))
		return
	}
	command.Stderr = command.Stdout

	if err := command.Start(); err != nil {
		s.write(red("could not start " + tool + ": " + err.Error()))
		return
	}

	// Line by line, rewriting the ends: a terminal needs a carriage return before each
	// newline, and kubectl writes for a file.
	reader := bufio.NewReader(stdout)
	buffer := make([]byte, 4096)
	for {
		n, readErr := reader.Read(buffer)
		if n > 0 {
			s.write(strings.ReplaceAll(strings.ReplaceAll(string(buffer[:n]), "\r\n", "\n"), "\n", "\r\n"))
		}
		if readErr != nil {
			break
		}
	}

	waitErr := command.Wait()
	switch {
	case errors.Is(runCtx.Err(), context.DeadlineExceeded):
		s.write(red(fmt.Sprintf("stopped after %s", s.opts.Timeout)))
	case errors.Is(runCtx.Err(), context.Canceled):
		s.write("\x1b[2m^C\x1b[0m\r\n")
	case waitErr != nil:
		var exit *exec.ExitError
		if errors.As(waitErr, &exit) {
			s.write(fmt.Sprintf("\x1b[2mexit %d\x1b[0m\r\n", exit.ExitCode()))
		}
	}
}

func (s *Session) refuse(line, reason string) {
	s.record(line, false)
	s.write(red(reason))
	s.prompt()
}

func (s *Session) record(command string, allowed bool) {
	if s.opts.OnCommand != nil {
		s.opts.OnCommand(command, allowed)
	}
}

func (s *Session) banner() {
	s.write("\x1b[2mKubernetes cluster \x1b[0m\x1b[36m" + s.opts.ClusterName +
		"\x1b[0m\x1b[2m is in context. kubectl and helm run here — type \x1b[0mhelp\x1b[2m for what that means.\x1b[0m\r\n\r\n")
}

func (s *Session) help() {
	s.write("\r\n" +
		"  \x1b[36mkubectl …\x1b[0m        run against \x1b[1m" + s.opts.ClusterName + "\x1b[0m with your own permissions\r\n" +
		"  \x1b[36mhelm …\x1b[0m           the same, for charts\r\n" +
		"  \x1b[2mdrop a file\x1b[0m      drag a manifest, values file or chart folder in here,\r\n" +
		"  \x1b[2m\x1b[0m                 then use its name: \x1b[36mkubectl apply -f ingress.yaml\x1b[0m\r\n" +
		"  \x1b[36mclear\x1b[0m            clear the screen  (Ctrl+L)\r\n" +
		"  \x1b[36mhelp\x1b[0m             this\r\n" +
		"  \x1b[2m↑ ↓\x1b[0m              earlier commands\r\n" +
		"  \x1b[2mCtrl+C\x1b[0m           stop what is running\r\n" +
		"\r\n" +
		"  \x1b[2mNothing else runs: this terminal is scoped to the cluster, not to the\r\n" +
		"  machine Kubby runs on. Dropped files live only as long as this tab.\r\n" +
		"  For a shell inside a pod, use the pod's Shell action.\x1b[0m\r\n" +
		"\r\n")
}

func (s *Session) prompt() {
	s.write("\x1b[32m$\x1b[0m ")
}

func (s *Session) write(text string) {
	_, _ = io.WriteString(s.out, text)
}

func red(text string) string {
	return "\x1b[31m" + text + "\x1b[0m\r\n"
}

func orDefault(value, fallback uint16) uint16 {
	if value == 0 {
		return fallback
	}
	return value
}

const (
	kubectlBinary = "kubectl"
	helmBinary    = "helm"
)

// allowedTools is the whole list. Both reach only the cluster this session is pointed
// at, and both are on PATH beside Kubby rather than anywhere the reader chooses.
var allowedTools = map[string]bool{kubectlBinary: true, helmBinary: true}

// mutatingVerbs pass only when the reader may write, through the same gate as every
// other action rather than a second one that could drift from it.
var mutatingVerbs = map[string]map[string]bool{
	kubectlBinary: {
		"annotate": true, "apply": true, "autoscale": true, "cordon": true, "create": true,
		"delete": true, "drain": true, "edit": true, "expose": true, "label": true,
		"patch": true, "replace": true, "rollout": true, "run": true, "scale": true,
		"set": true, "taint": true, "uncordon": true,
	},
	helmBinary: {
		"install": true, "upgrade": true, "uninstall": true, "rollback": true,
		"delete": true, "push": true, "create": true,
	},
}

// refusedVerbs never pass. Each one either reaches past the cluster to the machine Kubby
// runs on, or needs a terminal this session does not have. The value is the reason, which
// names the way that does work rather than only saying no.
var refusedVerbs = map[string]map[string]string{
	kubectlBinary: {
		"proxy":        "it would open a port on the machine Kubby runs on",
		"port-forward": "use the object's Port forward action, which tunnels through Kubby",
		"exec":         "use the pod's Shell action, which is recorded",
		"attach":       "use the pod's Shell action, which is recorded",
		"cp":           "it reads and writes files on the machine Kubby runs on",
		"debug":        "use the pod's Shell action, which offers a debug container",
		"plugin":       "a plugin is an arbitrary binary on the machine Kubby runs on",
	},
	helmBinary: {
		"plugin": "a plugin is an arbitrary binary on the machine Kubby runs on",
	},
}
