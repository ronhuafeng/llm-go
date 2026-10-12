package codexexec

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// GoalSandboxConfig is supplied only by the trusted Ubuntu GitHub-hosted
// launcher. These are public runtime/input directories, never model arguments.
// The fixed public OS trees are /usr, /bin, /lib and /lib64. Credentials must not
// be stored in those trees or in any explicitly admitted directory.
// ReadOnlyDirs should contain only controller-produced feedback, not its home,
// checkout, credentials or executable. New feedback files remain visible.
type GoalSandboxConfig struct {
	// ModuleCache is a prewarmed, credential-free module cache, mounted read-only.
	ModuleCache  string
	RuntimeDirs  []string
	ReadOnlyDirs []string
}

// goalSandboxCommand isolates the entire App Server from the publishing parent.
// This fixed Linux layout deliberately preserves provider networking. It does
// not isolate host TCP or abstract Unix sockets; model shells still use Codex's
// separate network-disabled native sandbox. No host root is mounted.
func (e *Executor) goalSandboxCommand(cmd *exec.Cmd) (*exec.Cmd, error) {
	if runtime.GOOS != "linux" || e.config.GoalSandbox == nil {
		return nil, errors.New("goal isolation requires the trusted Linux launcher")
	}
	launcher, err := exec.LookPath("bwrap")
	if err != nil {
		return nil, errors.New("goal isolation requires bubblewrap")
	}
	binary, err := exec.LookPath(cmd.Args[0])
	if err != nil {
		return nil, errors.New("goal runtime unavailable")
	}
	binary, err = filepath.Abs(binary)
	if err != nil {
		return nil, errors.New("goal runtime unavailable")
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		return nil, errors.New("goal runtime unavailable")
	}
	if insideGoalPath(binary, e.config.Workspace) || insideGoalPath(binary, e.home) {
		return nil, errors.New("goal runtime must be outside candidate and state")
	}
	git := filepath.Join(e.config.Workspace, ".git")
	gitInfo, err := os.Lstat(git)
	if err != nil || !gitInfo.IsDir() {
		return nil, errors.New("goal requires a standalone credential-free candidate repository")
	}
	cfg := e.config.GoalSandbox
	if !e.validGoalInput(cfg.ModuleCache) {
		return nil, errors.New("goal module cache unavailable")
	}
	if err := e.prepareGoalScratch(); err != nil {
		return nil, err
	}
	if len(cfg.RuntimeDirs) > 8 || len(cfg.ReadOnlyDirs) > 8 {
		return nil, errors.New("too many goal runtime inputs")
	}
	flags := []string{"--unshare-all", "--share-net", "--die-with-parent", "--new-session", "--tmpfs", "/"}
	for _, path := range []string{"/usr", "/bin", "/lib", "/lib64"} {
		if _, err := os.Stat(path); err != nil {
			return nil, errors.New("unsupported goal runtime layout")
		}
		flags = append(flags, "--ro-bind", path, path)
	}
	// Only public TLS and resolver inputs are admitted from /etc, never its
	// password databases, application configuration, SSH keys or credential files.
	for _, path := range []string{"/etc/ssl/certs", "/etc/resolv.conf", "/etc/hosts", "/etc/nsswitch.conf"} {
		if _, err := os.Stat(path); err != nil {
			return nil, errors.New("goal network runtime input unavailable")
		}
		flags = append(flags, "--ro-bind", path, path)
	}
	flags = append(flags, "--proc", "/proc", "--dev", "/dev", "--tmpfs", "/tmp", "--tmpfs", "/run", "--tmpfs", "/home")
	pathEntries := []string{"/usr/bin", "/bin"}
	for _, paths := range [][]string{cfg.RuntimeDirs, cfg.ReadOnlyDirs} {
		for _, path := range paths {
			if !e.validGoalInput(path) {
				return nil, errors.New("invalid trusted goal input")
			}
			flags = append(flags, "--ro-bind", path, path)
		}
	}
	for _, path := range cfg.RuntimeDirs {
		pathEntries = append([]string{filepath.Join(path, "bin"), path}, pathEntries...)
	}
	flags = append(flags, "--ro-bind", cfg.ModuleCache, cfg.ModuleCache)
	flags = append(flags, "--ro-bind", binary, binary,
		"--bind", e.config.Workspace, e.config.Workspace,
		"--ro-bind", git, git,
		"--bind", e.home, e.home,
		"--chdir", e.config.Workspace, "--", binary)
	flags = append(flags, cmd.Args[1:]...)
	cmd.Path = launcher
	cmd.Args = append([]string{launcher}, flags...)
	cmd.Env = replaceGoalEnv(cmd.Env, "HOME", e.home)
	cmd.Env = replaceGoalEnv(cmd.Env, "TMPDIR", "/tmp")
	cmd.Env = replaceGoalEnv(cmd.Env, "TMP", "/tmp")
	cmd.Env = replaceGoalEnv(cmd.Env, "TEMP", "/tmp")
	cmd.Env = replaceGoalEnv(cmd.Env, "PATH", strings.Join(pathEntries, string(os.PathListSeparator)))
	for key, value := range e.goalGoEnvironment() {
		if key != "TMPDIR" {
			cmd.Env = replaceGoalEnv(cmd.Env, key, value)
		}
	}
	// os/exec supplies only stdin/out/err pipes. No publishing-parent descriptors
	// or sockets are passed into the namespace.
	cmd.ExtraFiles = nil
	return cmd, nil
}

func (e *Executor) validGoalInput(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || strings.ContainsAny(path, "\x00\r\n:") {
		return false
	}
	switch path {
	case "/", "/etc", "/opt", "/home", "/root", "/tmp", "/run", "/workspace", "/usr", "/bin", "/lib", "/lib64":
		return false
	}
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		return false
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return false
	}
	for _, private := range []string{e.config.Workspace, e.home} {
		if insideGoalPath(private, path) || insideGoalPath(path, private) {
			return false
		}
	}
	return true
}

func insideGoalPath(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func replaceGoalEnv(env []string, key, value string) []string {
	result := make([]string, 0, len(env)+1)
	for _, entry := range env {
		if !strings.HasPrefix(entry, key+"=") {
			result = append(result, entry)
		}
	}
	return append(result, key+"="+value)
}

// NativeGoalRuntimePaths resolves the fixed hosted-runner runtime inputs for a
// trusted launcher. binary is never model-supplied. npm's JavaScript entry point
// is supported only inside the selected Node installation or public /usr tree;
// arbitrary npm prefixes require an explicitly reviewed launcher configuration.
func NativeGoalRuntimePaths(binary string) ([]string, error) {
	if binary == "" {
		binary = "codex"
	}
	executable, err := exec.LookPath(binary)
	if err != nil {
		return nil, errors.New("goal CLI unavailable")
	}
	executable, err = filepath.EvalSymlinks(executable)
	if err != nil {
		return nil, errors.New("goal CLI unavailable")
	}
	executable, err = filepath.Abs(executable)
	if err != nil {
		return nil, errors.New("goal CLI unavailable")
	}
	dirs := []string{filepath.Dir(executable)}
	if strings.HasSuffix(executable, ".js") {
		node, err := exec.LookPath("node")
		if err != nil {
			return nil, errors.New("goal Node runtime unavailable")
		}
		node, err = filepath.EvalSymlinks(node)
		if err != nil {
			return nil, errors.New("goal Node runtime unavailable")
		}
		root := filepath.Dir(filepath.Dir(node))
		if !insideGoalPath(executable, root) && !insideGoalPath(executable, "/usr") {
			return nil, errors.New("unsupported goal npm runtime layout")
		}
		if root != "/usr" {
			dirs = append(dirs, root)
		}
	}
	goroot, err := filepath.EvalSymlinks(runtime.GOROOT())
	if err != nil {
		return nil, errors.New("goal Go runtime unavailable")
	}
	dirs = append(dirs, goroot)
	return dirs, nil
}

func (e *Executor) goalGoEnvironment() map[string]string {
	return map[string]string{
		"GOMODCACHE": e.config.GoalSandbox.ModuleCache,
		"GOCACHE":    filepath.Join(e.config.Workspace, "codexsdk", ".cache", "codex-goal", "go-build"),
		"TMPDIR":     filepath.Join(e.config.Workspace, "codexsdk", ".cache", "codex-goal", "tmp"),
		"GOPROXY":    "off", "GOSUMDB": "off", "GOTOOLCHAIN": "local",
	}
}

func (e *Executor) goalShellOverrides() ([]string, error) {
	if e.config.GoalSandbox == nil || !e.validGoalInput(e.config.GoalSandbox.ModuleCache) {
		return nil, errors.New("goal module cache unavailable")
	}
	env := e.goalGoEnvironment()
	// Explicit set is required: inherit=core deliberately excludes ambient Go
	// settings. These writable paths remain INSIDE the candidate's existing grant.
	setting := fmt.Sprintf("{GOMODCACHE=%s,GOCACHE=%s,TMPDIR=%s,GOPROXY=\"off\",GOSUMDB=\"off\",GOTOOLCHAIN=\"local\"}", strconv.Quote(env["GOMODCACHE"]), strconv.Quote(env["GOCACHE"]), strconv.Quote(env["TMPDIR"]))
	return []string{"-c", "shell_environment_policy.set=" + setting}, nil
}

func (e *Executor) prepareGoalScratch() error {
	for _, parts := range [][]string{{"codexsdk"}, {"codexsdk", ".cache"}, {"codexsdk", ".cache", "codex-goal"}, {"codexsdk", ".cache", "codex-goal", "go-build"}, {"codexsdk", ".cache", "codex-goal", "tmp"}} {
		path := filepath.Join(append([]string{e.config.Workspace}, parts...)...)
		info, err := os.Lstat(path)
		if os.IsNotExist(err) {
			if os.Mkdir(path, 0700) != nil {
				return errors.New("goal scratch unavailable")
			}
			continue
		}
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("goal scratch must not traverse symlinks")
		}
	}
	return nil
}
