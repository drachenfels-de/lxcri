package lxcri

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/opencontainers/runtime-spec/specs-go"
	"gopkg.in/lxc/go-lxc.v2"
)

// Create creates a single container instance from the given ContainerConfig.
// Create is the first runtime method to call within the lifecycle of a container.
// You may have to call Runtime.Delete to cleanup container runtime state,
// if Create returns with an error.
func (rt *Runtime) Create(ctx context.Context, cfg *ContainerConfig) (*Container, error) {

	if err := rt.checkConfig(cfg); err != nil {
		return nil, err
	}

	c := &Container{ContainerConfig: cfg}
	c.runtimeDir = filepath.Join(rt.Root, c.ContainerID)

	if err := c.create(); err != nil {
		return c, errorf("failed to create container: %w", err)
	}

	if rt.OnCreate != nil {
		rt.OnCreate(ctx, c)
	}
	if c.OnCreate != nil {
		c.OnCreate(ctx, c)
	}

	if cfg.Spec.Hooks != nil {
		for i, hook := range cfg.Spec.Hooks.CreateRuntime {
			cmd := exec.Command(hook.Path, hook.Args...)
			cmd.Env = hook.Env
			out, err := cmd.CombinedOutput()
			if err != nil {
				rt.Log.Error().Msgf("failed to run hook CreateRuntime[%d]: %s", i, err)
			}
			println(string(out))
		}
	}

	if err := configureContainer(rt, c); err != nil {
		return c, errorf("failed to configure container: %w", err)
	}

	specPath := c.RuntimePath("spec.json")
	err := encodeFileJSON(specPath, cfg.Spec, os.O_EXCL|os.O_CREATE|os.O_RDWR, 0440)
	if err != nil {
		return c, err
	}

	if err := rt.runStartCmd(ctx, c); err != nil {
		return c, errorf("failed to run container process: %w", err)
	}

	p := c.RuntimePath("container.json")
	err = encodeFileJSON(p, c, os.O_EXCL|os.O_CREATE|os.O_RDWR, 0440)
	if err != nil {
		return c, err
	}

	return c, err
}

func (rt *Runtime) keepEnv(names ...string) {
	for _, n := range names {
		if val := os.Getenv(n); val != "" {
			rt.env = append(rt.env, n+"="+val)
		}
	}
}

// Init initializes the runtime instance.
// It creates required directories and checks the hosts system configuration.
// Unsupported runtime features are disabled and a warning message is logged.
// Init must be called once for a runtime instance before calling any other method.
func (rt *Runtime) Init() error {
	rt.rootfsMount = filepath.Join(rt.Root, ".rootfs")
	if err := os.MkdirAll(rt.rootfsMount, 0777); err != nil {
		return errorf("failed to create directory for rootfs mount %s: %w", rt.rootfsMount, err)
	}
	if err := os.Chmod(rt.rootfsMount, 0777); err != nil {
		return errorf("failed to 'chmod 0777 %s': %w", err)
	}

	rt.privileged = os.Getuid() == 0

	rt.keepEnv("HOME", "XDG_RUNTIME_DIR", "PATH")

	err := canExecute(rt.libexec(ExecStart), rt.libexec(ExecHook), rt.libexec(ExecInit))
	if err != nil {
		return errorf("access check failed: %w", err)
	}

	if err := isFilesystem("/proc", "proc"); err != nil {
		return errorf("procfs not mounted on /proc: %w", err)
	}
	if err := isFilesystem(cgroupRoot, "cgroup2"); err != nil {
		return errorf("ccgroup2 not mounted on %s: %w", cgroupRoot, err)
	}

	if !lxc.VersionAtLeast(3, 1, 0) {
		return errorf("liblxc runtime version is %s, but >= 3.1.0 is required", lxc.Version())
	}

	if !lxc.VersionAtLeast(4, 0, 5) {
		rt.Log.Warn().Msgf("liblxc runtime version >= 4.0.5 is recommended (was %s)", lxc.Version())
	}

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

	yes, err := isHostNamespaceShared(spec.Linux.Namespaces, specs.MountNamespace)
	if err != nil {
		return err
	}
	if yes {
		return errorf("container wants to share the hosts mount namespace")
	}

	// It should be best practise not to do so, but there are containers that
	// want to share the hosts PID namespaces. e.g sonobuoy/sonobuoy-systemd-logs-daemon-set
	yes, err = isHostNamespaceShared(spec.Linux.Namespaces, specs.PIDNamespace)
	if err != nil {
		return err
	}
	if yes {
		rt.Log.Info().Msg("container wil share the hosts PID namespace")
	}
	return nil
}

func configureContainer(rt *Runtime, c *Container) error {
	if c.Spec.Hostname != "" {
		if err := c.SetConfigItem("lxc.uts.name", c.Spec.Hostname); err != nil {
			return err
		}

		uts := getNamespace(specs.UTSNamespace, c.Spec.Linux.Namespaces)
		if uts != nil && uts.Path != "" {
			if err := setHostname(uts.Path, c.Spec.Hostname); err != nil {
				return fmt.Errorf("failed  to set hostname: %w", err)
			}
		}
	}

	if err := configureRootfs(rt, c); err != nil {
		return fmt.Errorf("failed to configure rootfs: %w", err)
	}

	if err := configureInit(rt, c); err != nil {
		return fmt.Errorf("failed to configure init: %w", err)
	}

	if !rt.privileged {
		// ensure user namespace is enabled
		if !isNamespaceEnabled(c.Spec, specs.UserNamespace) {
			rt.Log.Warn().Msg("unprivileged runtime - enabling user namespace")
			c.Spec.Linux.Namespaces = append(c.Spec.Linux.Namespaces,
				specs.LinuxNamespace{Type: specs.UserNamespace},
			)
		}
	}
	if err := configureNamespaces(c); err != nil {
		return fmt.Errorf("failed to configure namespaces: %w", err)
	}

	if c.Spec.Process.OOMScoreAdj != nil {
		if err := c.SetConfigItem("lxc.proc.oom_score_adj", fmt.Sprintf("%d", *c.Spec.Process.OOMScoreAdj)); err != nil {
			return err
		}
	}

	if c.Spec.Process.NoNewPrivileges {
		if err := c.SetConfigItem("lxc.no_new_privs", "1"); err != nil {
			return err
		}
	}

	if rt.Features.Apparmor {
		if err := configureApparmor(c); err != nil {
			return fmt.Errorf("failed to configure apparmor: %w", err)
		}
	} else {
		rt.Log.Warn().Msg("apparmor feature is disabled - profile is set to unconfined")
	}

	if rt.Features.Seccomp {
		if c.Spec.Linux.Seccomp != nil && len(c.Spec.Linux.Seccomp.Syscalls) > 0 {
			profilePath := c.RuntimePath("seccomp.conf")
			if err := writeSeccompProfile(profilePath, c.Spec.Linux.Seccomp); err != nil {
				return err
			}
			if err := c.SetConfigItem("lxc.seccomp.profile", profilePath); err != nil {
				return err
			}
		}
	} else {
		rt.Log.Warn().Msg("seccomp feature is disabled - all system calls are allowed")
	}

	if rt.Features.Capabilities {
		if err := configureCapabilities(c); err != nil {
			return fmt.Errorf("failed to configure capabilities: %w", err)
		}
	} else {
		rt.Log.Warn().Msg("capabilities feature is disabled - running with full privileges")
	}

	// make sure autodev is disabled
	if err := c.SetConfigItem("lxc.autodev", "0"); err != nil {
		return err
	}

	if err := ensureDefaultDevices(c.Spec); err != nil {
		return err
	}

	if rt.privileged {
		// devices are created with mknod in lxcri-hook
		if err := createDeviceFile(c.RuntimePath("devices.txt"), c.Spec); err != nil {
			return fmt.Errorf("failed to create devices.txt: %w", err)
		}

	} else {
		// if running as non-root bind mount devices, because user can not execute mknod
		newMounts := make([]specs.Mount, 0, len(c.Spec.Mounts)+len(c.Spec.Linux.Devices))
		for _, m := range c.Spec.Mounts {
			if m.Destination == "/dev" {
				rt.Log.Info().Msg("unprivileged runtime - removing /dev mount")
				continue
			}
			newMounts = append(newMounts, m)
		}
		rt.Log.Info().Msg("unprivileged runtime - bind mount devices")
		for _, device := range c.Spec.Linux.Devices {
			newMounts = append(newMounts,
				specs.Mount{Destination: device.Path, Source: device.Path, Type: "bind", Options: []string{"bind", "create=file"}},
			)
		}

		c.Spec.Mounts = newMounts
	}

	if err := writeMasked(c.RuntimePath("masked.txt"), c); err != nil {
		return fmt.Errorf("failed to create masked.txt: %w", err)
	}

	// pass context information as environment variables to hook scripts
	if err := c.SetConfigItem("lxc.hook.version", "1"); err != nil {
		return err
	}
	if err := c.SetConfigItem("lxc.hook.mount", rt.libexec(ExecHook)); err != nil {
		return err
	}

	if err := configureCgroup(rt, c); err != nil {
		return fmt.Errorf("failed to configure cgroups: %w", err)
	}

	for key, val := range c.Spec.Linux.Sysctl {
		if err := c.SetConfigItem("lxc.sysctl."+key, val); err != nil {
			return err
		}
	}

	// `man lxc.container.conf`: "A resource with no explicitly configured limitation will be inherited
	// from the process starting up the container"
	seenLimits := make([]string, 0, len(c.Spec.Process.Rlimits))
	for _, limit := range c.Spec.Process.Rlimits {
		name := strings.TrimPrefix(strings.ToLower(limit.Type), "rlimit_")
		for _, seen := range seenLimits {
			if seen == name {
				return fmt.Errorf("duplicate resource limit %q", limit.Type)
			}
		}
		seenLimits = append(seenLimits, name)
		val := fmt.Sprintf("%d:%d", limit.Soft, limit.Hard)
		if err := c.SetConfigItem("lxc.prlimit."+name, val); err != nil {
			return err
		}
	}

	if err := setLog(c); err != nil {
		return errorf("failed to configure container log: %w", err)
	}

	if err := configureMounts(rt, c); err != nil {
		return fmt.Errorf("failed to configure mounts: %w", err)
	}

	if err := configureReadonlyPaths(c); err != nil {
		return fmt.Errorf("failed to configure read-only paths: %w", err)
	}

	return nil
}

func configureRootfs(rt *Runtime, c *Container) error {
	if err := c.SetConfigItem("lxc.rootfs.path", c.Spec.Root.Path); err != nil {
		return err
	}

	if err := c.SetConfigItem("lxc.rootfs.mount", rt.rootfsMount); err != nil {
		return err
	}

	if err := c.SetConfigItem("lxc.rootfs.managed", "0"); err != nil {
		return err
	}

	// Resources not created by the container runtime MUST NOT be deleted by it.
	if err := c.SetConfigItem("lxc.ephemeral", "0"); err != nil {
		return err
	}

	rootfsOptions := []string{}
	if c.Spec.Linux.RootfsPropagation != "" {
		rootfsOptions = append(rootfsOptions, c.Spec.Linux.RootfsPropagation)
	}
	if c.Spec.Root.Readonly {
		rootfsOptions = append(rootfsOptions, "ro")
	}
	if err := c.SetConfigItem("lxc.rootfs.options", strings.Join(rootfsOptions, ",")); err != nil {
		return err
	}
	return nil
}

func configureReadonlyPaths(c *Container) error {
	rootmnt := c.GetConfigItem("lxc.rootfs.mount")
	if rootmnt == "" {
		return fmt.Errorf("lxc.rootfs.mount unavailable")
	}
	for _, p := range c.Spec.Linux.ReadonlyPaths {
		mnt := fmt.Sprintf("%s %s %s %s", filepath.Join(rootmnt, p), strings.TrimPrefix(p, "/"), "bind", "bind,ro,optional")
		if err := c.SetConfigItem("lxc.mount.entry", mnt); err != nil {
			return fmt.Errorf("failed to make path readonly: %w", err)
		}
	}
	return nil
}

func configureApparmor(c *Container) error {
	// The value *apparmor_profile*  from crio.conf is used if no profile is defined by the container.
	aaprofile := c.Spec.Process.ApparmorProfile
	if aaprofile == "" {
		aaprofile = "unconfined"
	}
	return c.SetConfigItem("lxc.apparmor.profile", aaprofile)
}

// configureCapabilities configures the linux capabilities / privileges granted to the container processes.
// See `man lxc.container.conf` lxc.cap.drop and lxc.cap.keep for details.
// https://blog.container-solutions.com/linux-capabilities-in-practice
// https://blog.container-solutions.com/linux-capabilities-why-they-exist-and-how-they-work
func configureCapabilities(c *Container) error {
	keepCaps := "none"
	if c.Spec.Process.Capabilities != nil {
		var caps []string
		for _, c := range c.Spec.Process.Capabilities.Permitted {
			lcCapName := strings.TrimPrefix(strings.ToLower(c), "cap_")
			caps = append(caps, lcCapName)
		}
		if len(caps) > 0 {
			keepCaps = strings.Join(caps, " ")
		}
	}

	return c.SetConfigItem("lxc.cap.keep", keepCaps)
}

func writeMasked(dst string, c *Container) error {
	// #nosec
	if c.Spec.Linux.MaskedPaths == nil {
		return nil
	}
	f, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	for _, p := range c.Spec.Linux.MaskedPaths {
		_, err = fmt.Fprintln(f, p)
		if err != nil {
			f.Close()
			return err
		}
	}
	return f.Close()
}
