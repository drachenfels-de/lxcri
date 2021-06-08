// Package lxcri provides an OCI specific runtime interface for lxc.
package lxcri

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/creack/pty"
	"github.com/drachenfels-de/gocapability/capability"
	"github.com/lxc/go-lxc"
	"github.com/lxc/lxcri/pkg/log"
	"github.com/lxc/lxcri/pkg/specki"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
	"sigs.k8s.io/yaml"
)

const (
	// BundleConfigFile is the name of the OCI container bundle config file.
	// The content is the JSON encoded specs.Spec.
	BundleConfigFile = "config.json"
)

// Required runtime executables loaded from Runtime.LibexecDir
var (
	// ExecStart starts the liblxc monitor process, similar to lxc-start
	ExecStart = "lxcri-start"
	// ExecHook is run as liblxc hook and creates additional devices and remounts masked paths.
	ExecHook        = "lxcri-hook"
	ExecHookBuiltin = "lxcri-hook-builtin"
	// ExecInit is the container init process that execs the container process.
	ExecInit = "lxcri-init"

	defaultLibexecDir = "/usr/libexec/lxcri"
)

var (
	// ErrNotExist is returned if the container (runtime dir) does not exist.
	ErrNotExist = fmt.Errorf("container does not exist")
)

// RuntimeFeatures are (security) features supported by the Runtime.
// The supported features are enabled on any Container instance
// created by Runtime.Create.
type RuntimeFeatures struct {
	Seccomp       bool
	Capabilities  bool
	Apparmor      bool
	CgroupDevices bool
}

// Runtime is a factory for creating and managing containers.
// The exported methods of Runtime  are required to implement the
// OCI container runtime interface spec (CRI).
// It shares the common settings
type Runtime struct {
	// Log is the logger used by the runtime.
	Log zerolog.Logger `json:"-"`

	// Root is the file path to the runtime directory.
	// Directories for containers created by the runtime
	// are created within this directory.
	Root string `json:",omitempty"`

	// MonitorCgroup is the path to the lxc monitor cgroup (lxc specific feature).
	// This is the cgroup where the liblxc monitor process (lxcri-start)
	// will be placed in. It's similar to /etc/crio/crio.conf#conmon_cgroup
	MonitorCgroup string `json:",omitempty"`

	// PayloadCgroup is the path to the default container payload cgroup.
	// This path is used if specs.Spec.Linux.CgroupsPaths is empty.
	PayloadCgroup string `json:",omitempty"`

	// LibexecDir is the the directory that contains the runtime executables.
	LibexecDir string `json:",omitempty"`

	// Featuress are runtime (security) features that apply to all containers
	// created by the runtime.
	Features RuntimeFeatures

	specs.Hooks `json:",omitempty"`

	// Environment passed to `lxcri-start`
	env []string

	caps capability.Capabilities

	// Runtime is running within a preconfigured user namespace.
	// This is set by `buildah` when runtime is called from a non-root user.
	// The user namespace must be dropped from the namespace list.
	// Runtime user detection using os.Getuid() or os.Geteuid() will not work.
	usernsConfigured bool

	LogConfig LogConfig
	Timeouts  Timeouts

	ConfigPath string `json:"-"`
}

// LogConfig is the runtime log configuration.
type LogConfig struct {
	file *os.File

	LogFile   string `json:",omitempty"`
	LogLevel  string `json:",omitempty"`
	Timestamp string `json:",omitempty"`

	LogConsole bool              `json:"-"`
	LogContext map[string]string `json:"-"`

	ContainerLogLevel string `json:",omitempty"`
	ContainerLogFile  string `json:",omitempty"`
}

// Timeouts are the timeouts for the Runtime API methods
type Timeouts struct {
	CreateTimeout uint `json:",omitempty"`
	StartTimeout  uint `json:",omitempty"`
	KillTimeout   uint `json:",omitempty"`
	DeleteTimeout uint `json:",omitempty"`
}

func (rt *Runtime) libexec(name string) string {
	return filepath.Join(rt.LibexecDir, name)
}

func (rt *Runtime) hasCapability(s string) bool {
	c, exist := capability.Parse(s)
	if !exist {
		rt.Log.Warn().Msgf("undefined capability %q", s)
		return false
	}
	return rt.caps.Get(capability.CAPS, c)
}

func (rt *Runtime) isPrivileged() bool {
	if rt.usernsConfigured {
		// FIXME this might be wrong if the runtime was started
		// in a preconfigured user namespace from root and
		// the uidmap maps the root user to itself.
		return false
	}
	// FIXME use os.Geteuid() ?
	return os.Getuid() == 0
}

// Init initializes the runtime instance.
// It creates required directories and checks the runtimes system configuration.
// Unsupported runtime features are disabled and a warning message is logged.
// Init must be called once for a runtime instance before calling any other method.
func (rt *Runtime) Init() error {
	if err := rt.configureLogger(); err != nil {
		return errorf("failed to configure logger: %w", err)
	}

	rt.Log.Debug().Msgf("Using runtime root %s", rt.Root)
	if err := os.MkdirAll(rt.Root, 0711); err != nil {
		return errorf("failed to create rootfs %s: %w", rt.Root, err)
	}

	_, rt.usernsConfigured = os.LookupEnv("_CONTAINERS_USERNS_CONFIGURED")

	caps, err := capability.NewPid2(0)
	if err != nil {
		return errorf("failed to create capabilities object: %w", err)
	}
	if err := caps.Load(); err != nil {
		return errorf("failed to load process capabilities: %w", err)
	}
	rt.caps = caps

	rt.keepEnv("HOME", "XDG_RUNTIME_DIR", "PATH", "LISTEN_FDS")

	err = canExecute(rt.libexec(ExecStart), rt.libexec(ExecHook), rt.libexec(ExecInit))
	if err != nil {
		return errorf("access check failed: %w", err)
	}

	if err := isFilesystem("/proc", "proc"); err != nil {
		return errorf("procfs not mounted on /proc: %w", err)
	}

	cgroupRoot, err = detectCgroupRoot(rt)
	if err != nil {
		rt.Log.Warn().Msgf("cgroup root detection failed: %s", err)
	}
	rt.Log.Info().Msgf("using cgroup root %s", cgroupRoot)

	if !lxc.VersionAtLeast(3, 1, 0) {
		return errorf("liblxc runtime version is %s, but >= 3.1.0 is required", lxc.Version())
	}

	if !lxc.VersionAtLeast(4, 0, 9) {
		rt.Log.Warn().Msgf("liblxc runtime version >= 4.0.9 is required for lxc.init.groups support (was %s)", lxc.Version())
	}

	rt.Hooks.CreateContainer = []specs.Hook{
		{Path: rt.libexec(ExecHookBuiltin)},
	}
	return nil
}

func (rt *Runtime) configureLogger() error {
	level, err := zerolog.ParseLevel(rt.LogConfig.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to parse log level: %w", err)
	}

	var logCtx zerolog.Context
	if rt.LogConfig.LogConsole {
		// TODO use console logger if filepath is /dev/stdout or /dev/stderr ?
		logCtx = log.ConsoleLogger(true, level)
		rt.LogConfig.ContainerLogFile = "/dev/stdout"
	} else {
		if err := os.MkdirAll(filepath.Dir(rt.LogConfig.LogFile), 0750); err != nil {
			return err
		}
		l, err := log.OpenFile(rt.LogConfig.LogFile, 0600)
		if err != nil {
			return fmt.Errorf("failed to open log file %q: %w", rt.LogConfig.LogFile, err)
		}
		rt.LogConfig.file = l
		logCtx = log.NewLogger(rt.LogConfig.file, level)
	}
	for k, v := range rt.LogConfig.LogContext {
		logCtx = logCtx.Str(k, v)
	}
	rt.Log = logCtx.Logger()

	return nil
}

func (rt *Runtime) checkConfig(cfg *ContainerConfig) error {
	if len(cfg.ContainerID) == 0 {
		return errorf("missing container ID")
	}
	return rt.checkSpec(cfg.Spec)
}

func (rt *Runtime) checkSpec(spec *specs.Spec) error {
	if spec.Root == nil {
		return errorf("spec.Root is nil")
	}
	if len(spec.Root.Path) == 0 {
		return errorf("empty spec.Root.Path")
	}

	if spec.Process == nil {
		return errorf("spec.Process is nil")
	}

	if len(spec.Process.Args) == 0 {
		return errorf("specs.Process.Args is empty")
	}

	if spec.Process.Cwd == "" {
		rt.Log.Info().Msg("specs.Process.Cwd is unset defaulting to '/'")
		spec.Process.Cwd = "/"
	}

	yes, err := isNamespaceSharedWithRuntime(getNamespace(spec, specs.MountNamespace))
	if err != nil {
		return errorf("failed to mount namespace: %s", err)
	}
	if yes {
		return errorf("container wants to share the runtimes mount namespace")
	}

	// It should be best practise not to do so, but there are containers that
	// want to share the runtimes PID namespaces. e.g sonobuoy/sonobuoy-systemd-logs-daemon-set
	yes, err = isNamespaceSharedWithRuntime(getNamespace(spec, specs.PIDNamespace))
	if err != nil {
		return errorf("failed to check PID namespace: %s", err)
	}
	if yes {
		rt.Log.Warn().Msg("container shares the PID namespace with the runtime")
	}
	return nil
}

func (rt *Runtime) keepEnv(names ...string) {
	for _, n := range names {
		if val, yes := os.LookupEnv(n); yes {
			rt.Log.Debug().Msgf("Keeping environment variable %q", n)
			rt.env = append(rt.env, n+"="+val)
		}
	}
}

// Load loads a container from the runtime directory.
// The container must have been created with Runtime.Create.
// The logger Container.Log is set to Runtime.Log by default.
// A loaded Container must be released with Container.Release after use.
func (rt *Runtime) Load(containerID string) (*Container, error) {
	rt.Log.Debug().Str("cid", containerID).Msg("loading container")
	dir := filepath.Join(rt.Root, containerID)
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, ErrNotExist
	}
	c := &Container{
		ContainerConfig: &ContainerConfig{
			Log: rt.Log.With().Str("cid", containerID).Logger(),
		},
		runtimeDir: dir,
	}
	if err := c.load(); err != nil {
		return nil, err
	}
	return c, nil
}

// Start starts the given container.
// Start simply unblocks the init process `lxcri-init`,
// which then executes the container process.
// The given container must have been created with Runtime.Create.
func (rt *Runtime) Start(ctx context.Context, c *Container) error {
	rt.Log.Info().Msg("notify init to start container process")

	state, err := c.State()
	if err != nil {
		return errorf("failed to get container state: %w", err)
	}
	if state.SpecState.Status != specs.StateCreated {
		return fmt.Errorf("invalid container state. expected %q, but was %q", specs.StateCreated, state.SpecState.Status)
	}

	err = c.start(ctx)
	if err != nil {
		return err
	}

	if c.Spec.Hooks != nil {
		state, err := c.State()
		if err != nil {
			return errorf("failed to get container state: %w", err)
		}
		specki.RunHooks(ctx, &state.SpecState, c.Spec.Hooks.Poststart, true)
	}
	return nil
}

func (rt *Runtime) runStartCmd(ctx context.Context, c *Container) (err error) {
	// #nosec
	cmd := exec.Command(rt.libexec(ExecStart), c.LinuxContainer.Name(), rt.Root, c.ConfigFilePath())
	cmd.Env = rt.env // environment variables required for liblxc
	cmd.Dir = c.Spec.Root.Path

	if c.ConsoleSocket == "" && !c.Spec.Process.Terminal {
		// Inherit stdio from calling process (conmon).
		// lxc.console.path must be set to 'none' or stdio of init process is replaced with a PTY by lxc
		if err := c.setConfigItem("lxc.console.path", "none"); err != nil {
			return err
		}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
	}

	// NOTE any config change via clxc.setConfigItem
	// must be done before calling SaveConfigFile
	err = c.LinuxContainer.SaveConfigFile(c.ConfigFilePath())
	if err != nil {
		return errorf("failed to save config file to %q: %w", c.ConfigFilePath(), err)
	}

	rt.Log.Debug().Msg("starting lxc monitor process")
	if c.ConsoleSocket != "" {
		err = rt.runStartCmdConsole(ctx, cmd, c.ConsoleSocket)
	} else {
		err = cmd.Start()
	}

	if err != nil {
		return err
	}

	c.CreatedAt = time.Now()
	c.Pid = cmd.Process.Pid
	rt.Log.Info().Int("pid", cmd.Process.Pid).Msg("monitor process started")

	p := c.RuntimePath("lxcri.json")
	err = specki.EncodeJSONFile(p, c, os.O_EXCL|os.O_CREATE, 0440)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	rt.Log.Debug().Msg("waiting for init")
	if err := c.waitCreated(ctx); err != nil {
		return err
	}

	return nil
}

func (rt *Runtime) runStartCmdConsole(ctx context.Context, cmd *exec.Cmd, consoleSocket string) error {
	rt.Log.Debug().Msgf("running command in console %s", consoleSocket)
	dialer := net.Dialer{}
	c, err := dialer.DialContext(ctx, "unix", consoleSocket)
	if err != nil {
		return fmt.Errorf("connecting to console socket failed: %w", err)
	}
	defer c.Close()

	conn, ok := c.(*net.UnixConn)
	if !ok {
		return fmt.Errorf("expected a unix connection but was %T", conn)
	}

	if deadline, ok := ctx.Deadline(); ok {
		err = conn.SetDeadline(deadline)
		if err != nil {
			return fmt.Errorf("failed to set connection deadline: %w", err)
		}
	}

	sockFile, err := conn.File()
	if err != nil {
		return fmt.Errorf("failed to get file from unix connection: %w", err)
	}
	ptmx, err := pty.Start(cmd)
	if err != nil {
		return fmt.Errorf("failed to start with pty: %w", err)
	}

	// Send the pty file descriptor over the console socket (to the 'conmon' process)
	// For technical backgrounds see:
	// * `man sendmsg 2`, `man unix 3`, `man cmsg 1`
	// * https://blog.cloudflare.com/know-your-scm_rights/
	oob := unix.UnixRights(int(ptmx.Fd()))
	// Don't know whether 'terminal' is the right data to send, but conmon doesn't care anyway.
	err = unix.Sendmsg(int(sockFile.Fd()), []byte("terminal"), oob, nil, 0)
	if err != nil {
		return fmt.Errorf("failed to send console fd: %w", err)
	}
	return ptmx.Close()
}

// Kill sends the signal signum to the container init process.
func (rt *Runtime) Kill(ctx context.Context, c *Container, signum unix.Signal) error {
	state, err := c.ContainerState()
	if err != nil {
		return err
	}
	if state == specs.StateStopped {
		return errorf("container already stopped")
	}
	return c.kill(ctx, signum)
}

// Delete removes the container from the runtime directory.
// The container must be stopped or force must be set to true.
// If the container is not stopped but force is set to true,
// the container will be killed with unix.SIGKILL.
func (rt *Runtime) Delete(ctx context.Context, containerID string, force bool) error {
	rt.Log.Info().Bool("force", force).Str("cid", containerID).Msg("delete container")
	c, err := rt.Load(containerID)
	if err == ErrNotExist {
		return err
	}
	if err != nil {
		// NOTE hooks won't run in this case
		rt.Log.Warn().Msgf("deleting runtime dir for unloadable container: %s", err)
		return os.RemoveAll(filepath.Join(rt.Root, containerID))
	}

	return c.Delete(ctx, force)
}

// Delete removes the container from the runtime directory.
func (c *Container) Delete(ctx context.Context, force bool) error {
	defer func() {
		if err := c.Release(); err != nil {
			c.Log.Error().Msgf("failed to release container: %s", err)
		}
	}()
	state, err := c.ContainerState()
	if err != nil {
		return err
	}
	if state != specs.StateStopped {
		c.Log.Debug().Msgf("delete state:%s", state)
		if !force {
			return errorf("container is not not stopped (current state %s)", state)
		}
		if err := c.kill(ctx, unix.SIGKILL); err != nil {
			return errorf("failed to kill container: %w", err)
		}
	}

	if err := c.waitMonitorStopped(ctx); err != nil {
		c.Log.Error().Msgf("failed to stop monitor process %d: %s", c.Pid, err)
	}

	// From OCI runtime spec
	// "Note that resources associated with the container, but not
	// created by this container, MUST NOT be deleted."
	// The *lxc.Container is created with `rootfs.managed=0`,
	// so calling *lxc.Container.Destroy will not delete container resources.
	if err := c.LinuxContainer.Destroy(); err != nil {
		return fmt.Errorf("failed to destroy container: %w", err)
	}

	// the monitor might be part of the cgroup so wait for it to exit
	eventsFile := filepath.Join(cgroupRoot, c.CgroupDir, "cgroup.events")
	err = pollCgroupEvents(ctx, eventsFile, func(ev cgroupEvents) bool {
		return !ev.populated
	})
	if err != nil && !os.IsNotExist(err) {
		// try to delete the cgroup anyways
		c.Log.Warn().Msgf("failed to wait until cgroup.events populated=0: %s", err)
	}

	err = deleteCgroup(c.CgroupDir)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete cgroup: %s", err)
	}

	if c.Spec.Hooks != nil {
		state, err := c.State()
		if err != nil {
			return errorf("failed to get container state: %w", err)
		}
		specki.RunHooks(ctx, &state.SpecState, c.Spec.Hooks.Poststop, true)
	}

	return os.RemoveAll(c.RuntimePath())
}

// List returns the IDs for all existing containers.
func (rt *Runtime) List() ([]string, error) {
	dir, err := os.Open(rt.Root)
	if err != nil {
		return nil, err
	}
	defer dir.Close()
	names, err := dir.Readdirnames(-1)
	if err != nil {
		return nil, err
	}
	// ignore hidden elements
	visible := make([]string, 0, len(names))
	for _, name := range names {
		if name[0] != '.' {
			visible = append(visible, name)
		}
	}
	return visible, nil
}

// DefaultRuntime is the default Runtime configuration.
var DefaultRuntime = Runtime{
	Root:          "/run/lxcri",
	MonitorCgroup: "lxcri-monitor.slice",
	PayloadCgroup: "lxcri.slice",
	LibexecDir:    defaultLibexecDir,
	Features: RuntimeFeatures{
		Apparmor:      true,
		Capabilities:  true,
		CgroupDevices: true,
		Seccomp:       true,
	},
	LogConfig: LogConfig{
		LogFile:           "/var/log/lxcri/lxcri.log",
		LogLevel:          "info",
		ContainerLogFile:  "/var/log/lxcri/lxcri.log",
		ContainerLogLevel: "warn",
	},

	Timeouts: Timeouts{
		CreateTimeout: 60,
		StartTimeout:  30,
		KillTimeout:   10,
		DeleteTimeout: 10,
	},
}

// NewRuntime creates a new runtime instance.
// The DefaultRuntime is returned as is if user is false,
// otherwise the runtime root and log file paths are set
// to user specific paths.
func NewRuntime(user bool) *Runtime {
	rt := DefaultRuntime
	if user {
		base := fmt.Sprintf("/var/tmp/lxcri/user/%d", os.Getuid())
		rt.Root = filepath.Join(base, "run")
		log := filepath.Join(base, "lxcri.log")
		rt.LogConfig.LogFile = log
		rt.LogConfig.ContainerLogFile = log
	}
	return &rt
}

// Release releases resources aquired by the runtime instance.
// E.g the runtime log file.
func (rt *Runtime) Release() error {
	if rt.LogConfig.file != nil {
		return rt.LogConfig.file.Close()
	}
	return nil
}

// LoadConfig loads the runtime configuration file.
// Values set in the config file overwrite the defaults from DefaultRuntime.
// Tthe first existing configuration file is used, and the
// configuration file path is evaluated in the following order:
//
// 1. the value of the `LXCRI_CONFIG` environment variable
// 2. the users config file `~/.config/lxcri.yaml`
// 3. The system config file `/etc/lxcri/lxcri.yaml`
func (rt *Runtime) LoadConfig(ConfigPath string) error {
	rt.ConfigPath = ConfigPath
	if rt.ConfigPath == "" {
		rt.ConfigPath = defaultConfigPath()
	}
	if err := rt.loadConfig(); err != nil {
		return err
	}
	return nil
}

func defaultConfigPath() string {
	if val, ok := os.LookupEnv("LXCRI_CONFIG"); ok && val != "" {
		return val
	}
	if val, ok := os.LookupEnv("HOME"); ok && val != "" {
		cfgFile := filepath.Join(val, ".config/lxcri.yaml")
		if _, err := os.Stat(cfgFile); err == nil {
			return cfgFile
		}
	}
	return "/etc/lxcri/lxcri.yaml"
}

func (rt *Runtime) loadConfig() error {
	if rt.ConfigPath == "" {
		return nil
	}

	data, err := os.ReadFile(rt.ConfigPath)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	return yaml.Unmarshal(data, rt)
}
