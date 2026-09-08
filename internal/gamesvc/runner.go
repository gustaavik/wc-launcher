package gamesvc

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
)

// Status is what the UI knows about the game process.
type Status struct {
	Running bool `json:"running"`
	PID     int  `json:"pid"`
	// ExitCode is meaningful only once a run has finished. Note that the game
	// exits 0 for almost everything: a bad join address, a failed host bind and
	// a failed world load are all logged and downgraded, leaving the process on
	// the menu. Staying up is the real success signal.
	ExitCode int    `json:"exitCode"`
	Message  string `json:"message"`
}

// ErrAlreadyRunning is returned when a second launch is attempted.
//
// Two copies sharing one data directory would race on the same saves and the
// same profile.toml.
var ErrAlreadyRunning = errors.New("Wyvencraft is already running")

// ErrNoVulkan is returned when no Vulkan driver could be found.
var ErrNoVulkan = fmt.Errorf("no Vulkan driver found — %s", VulkanHint)

// LogFunc receives the child's stderr, a line at a time.
type LogFunc func(line string)

// ExitFunc is called once, after the process ends.
type ExitFunc func(Status)

// Runner owns at most one game process.
type Runner struct {
	mu   sync.Mutex
	cmd  *exec.Cmd
	last Status
}

func NewRunner() *Runner { return &Runner{} }

// Status reports what the process is doing.
func (r *Runner) Status() Status {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.last
}

// Running reports whether a game process is alive.
//
// The launcher must not refresh the session while it is: the game refreshes the
// same token family, and two concurrent refreshes read as reuse server-side —
// which revokes every session for the account.
func (r *Runner) Running() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.cmd != nil
}

// Start launches the game and returns once it is running.
//
// The process is watched on a goroutine; onExit fires when it ends.
func (r *Runner) Start(opts Options, logPath string, onLog LogFunc, onExit ExitFunc) error {
	r.mu.Lock()
	if r.cmd != nil {
		r.mu.Unlock()
		return ErrAlreadyRunning
	}
	r.mu.Unlock()

	binary := filepath.Join(opts.VersionDir, gameBinaryName())
	info, err := os.Stat(binary)
	if err != nil {
		return fmt.Errorf("no game installed at %s", opts.VersionDir)
	}
	// A build unpacked by an older launcher, or restored from a backup that
	// dropped the mode bits, would fail with a bare "permission denied".
	if info.Mode()&0o111 == 0 {
		if err := os.Chmod(binary, 0o755); err != nil {
			return fmt.Errorf("%s is not executable: %w", binary, err)
		}
	}

	vulkan, ok := probeVulkan(opts.VersionDir, opts.MoltenVKDir)
	if !ok {
		return ErrNoVulkan
	}

	cmd := exec.Command(binary)
	// Mandatory. Every asset path in the game is resolved against the working
	// directory, so inheriting the launcher's would start a game with no
	// textures, no models and no content tables.
	cmd.Dir = opts.VersionDir
	cmd.Env = buildEnv(Environ(), opts, vulkan)

	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("capture the game's output: %w", err)
	}
	// The game logs everything to stderr; stdout is left attached to nothing.
	cmd.Stdout = nil

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start the game: %w", err)
	}

	r.mu.Lock()
	r.cmd = cmd
	r.last = Status{Running: true, PID: cmd.Process.Pid}
	r.mu.Unlock()

	go r.watch(cmd, stderr, logPath, onLog, onExit)
	return nil
}

// Stop ends the game process. A no-op when nothing is running.
func (r *Runner) Stop() error {
	r.mu.Lock()
	cmd := r.cmd
	r.mu.Unlock()

	if cmd == nil || cmd.Process == nil {
		return nil
	}
	return cmd.Process.Kill()
}

func (r *Runner) watch(cmd *exec.Cmd, stderr io.ReadCloser, logPath string, onLog LogFunc, onExit ExitFunc) {
	// Truncated per run. A launcher log that grows forever is a support burden,
	// and only the most recent session is ever useful.
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o644)
	if err != nil {
		logFile = nil
	}

	scanner := bufio.NewScanner(stderr)
	// Some log lines (a Vulkan validation dump, a panic backtrace) are long.
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if logFile != nil {
			fmt.Fprintln(logFile, line)
		}
		if onLog != nil {
			onLog(line)
		}
	}
	if logFile != nil {
		logFile.Close()
	}

	waitErr := cmd.Wait()

	status := Status{Running: false}
	var exitErr *exec.ExitError
	switch {
	case waitErr == nil:
		status.ExitCode = 0
		status.Message = "Wyvencraft closed."
	case errors.As(waitErr, &exitErr):
		status.ExitCode = exitErr.ExitCode()
		status.Message = fmt.Sprintf("Wyvencraft exited with code %d. See the log for details.", status.ExitCode)
	default:
		status.ExitCode = -1
		status.Message = "Wyvencraft stopped unexpectedly: " + waitErr.Error()
	}

	r.mu.Lock()
	r.cmd = nil
	r.last = status
	r.mu.Unlock()

	if onExit != nil {
		onExit(status)
	}
}
