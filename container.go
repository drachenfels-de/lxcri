package lxcri

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lxc/go-lxc"
	"github.com/lxc/lxcri/pkg/specki"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/rs/zerolog"
	"golang.org/x/sys/unix"
)

// ContainerConfig is the configuration for a single Container instance.
type ContainerConfig struct {
	// The Spec used to generate the liblxc config file.
	// Any changes to the spec after creating the liblxc config file have no effect
	// and should be avoided.
	// NOTE The Spec must be serialized with the runtime config (lxcri.json)
	// This is required because Spec.Annotations are required for Container.State()
	// and spec.Namespaces are required for attach.
	Spec *specs.Spec

	// ContainerID is the identifier of the container.
	// The ContainerID is used as name for the containers runtime directory.
	// The ContainerID must be unique at least through all containers of a runtime.
	// The ContainerID should match the following pattern `[a-z][a-z0-9-_]+`
	ContainerID string

	// BundlePath is the OCI bundle path.
	BundlePath string

	ConsoleSocket string `json:",omitempty"`

	// MonitorCgroupDir is the cgroup directory path
	// for the liblxc monitor process `lxcri-start`
	// relative to the cgroup root.
	MonitorCgroupDir string

	CgroupDir string

	// Use systemd encoded cgroup path (from crio-o/conmon)
	// is true if /etc/crio/crio.conf#cgroup_manager = "systemd"
	SystemdCgroup bool

	// LogFile is the liblxc log file path
	LogFile string

	// LogLevel is the liblxc log level
	LogLevel string

	// Log is the container Logger
	Log zerolog.Logger `json:"-"`
}

// ConfigFilePath returns the path to the liblxc config file.
func (c Container) ConfigFilePath() string {
	return c.RuntimePath("config")
}

func (c Container) syncFifoPath() string {
	return c.RuntimePath("syncfifo")
}

// RuntimePath returns the absolute path to the given sub path
// within the container runtime directory.
func (c Container) RuntimePath(subPath ...string) string {
	return filepath.Join(c.runtimeDir, filepath.Join(subPath...))
}

// Container is the runtime state of a container instance.
type Container struct {
	LinuxContainer *lxc.Container `json:"-"`
	*ContainerConfig

	CreatedAt time.Time
	// Pid is the process ID of the liblxc monitor process ( see ExecStart )
	Pid int

	runtimeDir string
}

func (c *Container) create() error {
	if err := os.MkdirAll(c.runtimeDir, 0777); err != nil {
		return fmt.Errorf("failed to create container dir: %w", err)
	}

	if err := os.Chmod(c.runtimeDir, 0777); err != nil {
		return errorf("failed to chmod %s: %w", err)
	}

	f, err := os.OpenFile(c.RuntimePath("config"), os.O_EXCL|os.O_CREATE|os.O_RDWR, 0640)
	if err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("failed to close empty config tmpfile: %w", err)
	}

	c.LinuxContainer, err = lxc.NewContainer(c.ContainerID, filepath.Dir(c.runtimeDir))
	if err != nil {
		return err
	}

	return nil
}

func (c *Container) load() error {
	err := specki.DecodeJSONFile(c.RuntimePath("lxcri.json"), c)
	if err != nil {
		return fmt.Errorf("failed to load container config: %w", err)
	}

	_, err = os.Stat(c.ConfigFilePath())
	if err != nil {
		return fmt.Errorf("failed to load lxc config file: %w", err)
	}
	c.LinuxContainer, err = lxc.NewContainer(c.ContainerID, filepath.Dir(c.runtimeDir))
	if err != nil {
		return fmt.Errorf("failed to create lxc container: %w", err)
	}

	err = c.LinuxContainer.LoadConfigFile(c.ConfigFilePath())
	if err != nil {
		return fmt.Errorf("failed to load config file: %w", err)
	}
	return nil
}

func (c *Container) waitMonitorStopped(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if !c.isMonitorRunning() {
				return nil
			}
			time.Sleep(time.Millisecond * 100)
		}
	}
}

func (c *Container) isMonitorRunning() bool {
	if c.Pid < 2 {
		return false
	}

	var ws unix.WaitStatus
	pid, err := unix.Wait4(c.Pid, &ws, unix.WNOHANG, nil)
	if pid == c.Pid {
		c.Log.Info().Msgf("monitor %d died: exited:%t exit_status:%d signaled:%t signal:%s",
			c.Pid, ws.Exited(), ws.ExitStatus(), ws.Signaled(), ws.Signal())
		return false
	}

	// if WNOHANG was specified and one or more child(ren) specified by pid exist,
	// but have not yet exited, then 0 is returned
	if pid == 0 {
		return true
	}

	// This runtime process may not be the parent of the monitor process
	if err == unix.ECHILD {
		// check if the process is still runnning
		err := unix.Kill(c.Pid, 0)
		if err == nil {
			return true
		}
		// it's not running
		if err == unix.ESRCH {
			return false
		}
	}
	return false
}

func (c *Container) waitCreated(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if !c.isMonitorRunning() {
				return fmt.Errorf("monitor already died")
			}
			state := c.LinuxContainer.State()
			if !(state == lxc.RUNNING) {
				c.Log.Debug().Stringer("state", state).Msg("wait for state lxc.RUNNING")
				time.Sleep(time.Millisecond * 100)
				continue
			}
			initState, err := c.getContainerInitState()
			if err != nil {
				return err
			}
			if initState == specs.StateCreated {
				return nil
			}
			return fmt.Errorf("unexpected init state %q", initState)
		}
	}
}

func (c *Container) waitStarted(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			if !c.isMonitorRunning() {
				return nil
			}
			initState, _ := c.getContainerInitState()
			if initState != specs.StateCreated {
				return nil
			}
			time.Sleep(time.Millisecond * 10)
		}
	}
}

// State wraps specs.State and adds runtime specific state.
type State struct {
	ContainerState string
	RuntimePath    string
	SpecState      specs.State
}

// State returns the runtime state of the containers process.
// The State.Pid value is the PID of the liblxc
// container monitor process (lxcri-start).
func (c *Container) State() (*State, error) {
	status, err := c.ContainerState()
	if err != nil {
		return nil, errorf("failed go get container status: %w", err)
	}

	state := &State{
		ContainerState: c.LinuxContainer.State().String(),
		RuntimePath:    c.RuntimePath(),
		SpecState: specs.State{
			Version:     c.Spec.Version,
			ID:          c.ContainerID,
			Bundle:      c.RuntimePath(),
			Pid:         c.Pid,
			Annotations: c.Spec.Annotations,
			Status:      status,
		},
	}

	return state, nil
}

// ContainerState returns the current state of the container process,
// as defined by the OCI runtime spec.
func (c *Container) ContainerState() (specs.ContainerState, error) {
	return c.state(c.LinuxContainer.State())
}

func (c *Container) state(s lxc.State) (specs.ContainerState, error) {
	switch s {
	case lxc.STOPPED:
		return specs.StateStopped, nil
	case lxc.STARTING:
		return specs.StateCreating, nil
	case lxc.RUNNING, lxc.STOPPING, lxc.ABORTING, lxc.FREEZING, lxc.FROZEN, lxc.THAWED:
		return c.getContainerInitState()
	default:
		return specs.StateStopped, fmt.Errorf("unsupported lxc container state %q", s)
	}
}

// getContainerInitState returns the detailed state of the container init process.
// This should be called if the container is in state lxc.RUNNING.
// On error the caller should call getContainerState() again
func (c *Container) getContainerInitState() (specs.ContainerState, error) {
	initPid := c.LinuxContainer.InitPid()
	if initPid < 1 {
		return specs.StateStopped, nil
	}
	cmdlinePath := fmt.Sprintf("/proc/%d/cmdline", initPid)
	cmdline, err := os.ReadFile(cmdlinePath)
	// Ignore any error here. Most likely the error will be os.ErrNotExist.
	// But I've seen race conditions where ESRCH is returned instead because
	// the process has died while opening it's proc directory.
	if err != nil {
		if !(os.IsNotExist(err) || err == unix.ESRCH) {
			c.Log.Warn().Str("file", cmdlinePath).Msgf("open failed: %s", err)
		}
		// init process died or returned
		return specs.StateStopped, nil
	}
	if string(cmdline) == "/.lxcri/lxcri-init\000" {
		return specs.StateCreated, nil
	}
	return specs.StateRunning, nil
}

func (c *Container) kill(ctx context.Context, signum unix.Signal) error {
	c.Log.Info().Int("signum", int(signum)).Msg("killing container processes")

	// From `man pid_namespaces`: If the "init" process of a PID namespace terminates, the kernel
	// terminates all of the processes in the namespace via a SIGKILL signal.
	// NOTE: The liblxc monitor process `lxcri-start` doesn't propagate all signals to the init process,
	// but handles some signals on its own. E.g SIGHUP tells the monitor process to hang up the terminal
	// and terminate the init process with SIGTERM.
	err := killCgroup(ctx, c, signum)

	// The cgroup could be deleted by liblxc while operating on it,
	// e.g if the container process(es) terminate prematurely.
	if os.IsNotExist(err) {
		return nil
	}
	if errors.Is(err, unix.ENODEV) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to kill group: %s", err)
	}
	return nil
}

// getConfigItem is a wrapper function and returns the
// first value returned by lxc.Container.ConfigItem
func (c *Container) getConfigItem(key string) string {
	vals := c.LinuxContainer.ConfigItem(key)
	if len(vals) > 0 {
		first := vals[0]
		// some lxc config values are set to '(null)' if unset eg. lxc.cgroup.dir
		// TODO check if this is already fixed
		if first != "(null)" {
			return first
		}
	}
	return ""
}

// setConfigItem is a wrapper for lxc.Container.setConfigItem.
// and only adds additional logging.
func (c *Container) setConfigItem(key, value string) error {
	err := c.LinuxContainer.SetConfigItem(key, value)
	if err != nil {
		return fmt.Errorf("failed to set config item '%s=%s': %w", key, value, err)
	}
	c.Log.Debug().Str(key, value).Msg("set config item")
	return nil
}

// supportsConfigItem is a wrapper for lxc.Container.IsSupportedConfig item.
func (c *Container) supportsConfigItem(keys ...string) bool {
	canCheck := lxc.VersionAtLeast(4, 0, 6)
	if !canCheck {
		c.Log.Warn().Msg("lxc.IsSupportedConfigItem is broken in liblxc < 4.0.6")
	}
	for _, key := range keys {
		if canCheck && lxc.IsSupportedConfigItem(key) {
			continue
		}
		c.Log.Info().Str("lxc.config", key).Msg("unsupported config item")
		return false
	}
	return true
}

// Release releases resources allocated by the container.
func (c *Container) Release() error {
	c.Log.Debug().Msg("releasing container")
	return c.LinuxContainer.Release()
}

func (c *Container) start(ctx context.Context) error {
	// #nosec
	fifo, err := os.OpenFile(c.syncFifoPath(), os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	if err := fifo.Close(); err != nil {
		return err
	}
	return c.waitStarted(ctx)
}

// ExecOptions contains options for Container.Exec and Container.ExecDetached
type ExecOptions struct {
	// Namespaces is the list of container namespaces that the process is attached to.
	// The process will is attached to all container namespaces if Namespaces is empty.
	Namespaces []specs.LinuxNamespaceType
}

// ExecDetached executes the given process spec within the container.
// The given process is started and the process PID is returned.
// It's up to the caller to wait for the process to exit using the returned PID.
// The container state must be either specs.StateCreated or specs.StateRunning
// The given ExecOptions execOpts, control the execution environment of the the process.
func (c *Container) ExecDetached(proc *specs.Process, execOpts *ExecOptions) (pid int, err error) {
	opts, err := c.attachOptions(proc, execOpts)
	if err != nil {
		return 0, errorf("failed to create attach options: %w", err)
	}

	pid, err = c.LinuxContainer.RunCommandNoWait(proc.Args, opts)
	if err != nil {
		return pid, errorf("failed to run exec cmd detached: %w", err)
	}
	return pid, nil
}

// Exec executes the given process spec within the container.
// It waits for the process to exit and returns its exit code.
// The container state must either be specs.StateCreated or specs.StateRunning
// The given ExecOptions execOpts control the execution environment of the the process.
func (c *Container) Exec(proc *specs.Process, execOpts *ExecOptions) (exitStatus int, err error) {
	opts, err := c.attachOptions(proc, execOpts)
	if err != nil {
		return 0, errorf("failed to create attach options: %w", err)
	}
	exitStatus, err = c.LinuxContainer.RunCommandStatus(proc.Args, opts)
	if err != nil {
		return exitStatus, errorf("failed to run exec cmd: %w", err)
	}
	return exitStatus, nil
}

func (c *Container) attachOptions(procSpec *specs.Process, execOpts *ExecOptions) (lxc.AttachOptions, error) {
	opts := lxc.AttachOptions{
		StdinFd:  0,
		StdoutFd: 1,
		StderrFd: 2,
	}

	if procSpec == nil {
		return opts, fmt.Errorf("process spec is nil")
	}
	opts.Cwd = procSpec.Cwd
	// Use the environment defined by the process spec.
	opts.ClearEnv = true
	opts.Env = procSpec.Env

	opts.UID = int(procSpec.User.UID)
	opts.GID = int(procSpec.User.GID)
	if n := len(procSpec.User.AdditionalGids); n > 0 {
		opts.Groups = make([]int, n)
		for i, g := range procSpec.User.AdditionalGids {
			opts.Groups[i] = int(g)
		}
	}

	if execOpts == nil {
		execOpts = new(ExecOptions)
	}

	if len(execOpts.Namespaces) == 0 {
		for t := range namespaceMap {
			execOpts.Namespaces = append(execOpts.Namespaces, t)
		}
	}
	c.Log.Debug().Msgf("attaching to namespaces %#v\n", execOpts.Namespaces)

	for _, n := range c.Spec.Linux.Namespaces {
		for _, t := range execOpts.Namespaces {
			if n.Type == t {
				if n, ok := namespaceMap[t]; ok {
					opts.Namespaces |= n.CloneFlag
				}
			}
		}
	}

	return opts, nil
}

// SetLog changes log file path and log level of the container (liblxc) instance.
// The settings are only valid until Release is called on this instance.
// The log settings applied at Runtime.Create are active until SetLog is called.
func (c *Container) SetLog(filename string, level string) error {
	// Do not write to stdout by default.
	// Stdout belongs to the container process.
	// Explicitly disable it - although it is currently the default.

	lxcLevel := parseContainerLogLevel(level)

	// FIXME control verbosity (configuration setting ...)
	verbose := false
	if lxcLevel == lxc.TRACE {
		if filename == "/dev/stderr" || filename == "/dev/stdout" ||
			filename == "/proc/self/fd/1" || filename == "/proc/self/fd/2" {
			verbose = true
		}
	}

	if verbose {
		c.LinuxContainer.SetVerbosity(lxc.Verbose)
	} else {
		c.LinuxContainer.SetVerbosity(lxc.Verbose)
	}
	err := c.LinuxContainer.SetLogLevel(lxcLevel)
	if err != nil {
		return fmt.Errorf("failed to set container loglevel: %w", err)
	}
	if err := c.LinuxContainer.SetLogFile(filename); err != nil {
		return fmt.Errorf("failed to set container log file: %w", err)
	}
	return nil
}

func parseContainerLogLevel(level string) lxc.LogLevel {
	switch strings.ToLower(level) {
	case "trace":
		return lxc.TRACE
	case "debug":
		return lxc.DEBUG
	case "info":
		return lxc.INFO
	case "notice":
		return lxc.NOTICE
	case "warn":
		return lxc.WARN
	case "error":
		return lxc.ERROR
	case "crit":
		return lxc.CRIT
	case "alert":
		return lxc.ALERT
	case "fatal":
		return lxc.FATAL
	default:
		return lxc.WARN
	}
}
