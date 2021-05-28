package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"

	"github.com/lxc/lxcri/pkg/log"
	"github.com/rs/zerolog"
	/*
		"github.com/lxc/lxcri"
		"github.com/lxc/lxcri/pkg/specki"
		"github.com/opencontainers/runtime-spec/specs-go"
	*/)

/*
see  https://github.com/containers/conmon/blob/31614525ebc5fd9668a6e084b5638d71b903bf6d/src/cli.c#L54

Usage:
  conmon [OPTION?] - conmon utility

Help Options:
  -h, --help                  Show help options

Application Options:
  --api-version               Conmon API version to use
  -b, --bundle                Location of the OCI Bundle path
  -c, --cid                   Identification of Container
  -P, --conmon-pidfile        PID file for the initial pid inside of container
  -p, --container-pidfile     PID file for the conmon process
  -u, --cuuid                 Container UUID
  -e, --exec                  Exec a command into a running container
  --exec-attach               Attach to an exec session
  --exec-process-spec         Path to the process spec for execution
  --exit-command              Path to the program to execute when the container terminates its execution
  --exit-command-arg          Additional arg to pass to the exit command.  Can be specified multiple times
  --exit-delay                Delay before invoking the exit command (in seconds)
  --exit-dir                  Path to the directory where exit files are written
  --leave-stdin-open          Leave stdin open when attached client disconnects
  --log-level                 Print debug logs based on log level
  -l, --log-path              Log file path
  --log-size-max              Maximum size of log file
  --log-tag                   Additional tag to use for logging
  -n, --name                  Container name
  --no-new-keyring            Do not create a new session keyring for the container
  --no-pivot                  Do not use pivot_root
  --no-sync-log               Do not manually call sync on logs after container shutdown
  -0, --persist-dir           Persistent directory for a container that can be used for storing container data
  --replace-listen-pid        Replace listen pid if set for oci-runtime pid
  --restore                   Restore a container from a checkpoint
  -r, --runtime               Path to store runtime data for the container
  --runtime-arg               Additional arg to pass to the runtime. Can be specified multiple times
  --runtime-opt               Additional opts to pass to the restore or exec command. Can be specified multiple times
  --sdnotify-socket           Path to the host's sd-notify socket to relay messages to
  --socket-dir-path           Location of container attach sockets
  -i, --stdin                 Open up a pipe to pass stdin to the container
  --sync                      Keep the main conmon process as its child by only forking once
  --syslog                    Log to syslog (use with cgroupfs cgroup manager)
  -s, --systemd-cgroup        Enable systemd cgroup manager, rather then use the cgroupfs directly
  -t, --terminal              Allocate a pseudo-TTY. The default is false
  -T, --timeout               Kill container after specified timeout in seconds.
  --version                   Print the version and exit

{"l":"debug","t":"17:43:20.773","c":"main.go:86","m":"[]string{\"/usr/local/libexec/lxcri/lxcri-conmon\", \"-b\", \"/var/lib/containers/run/overlay-containers/53799e2601e9c7ff6f70489034f8a31887395acb77078a92c03ff441f57edf69/userdata\", \"-c\", \"53799e2601e9c7ff6f70489034f8a31887395acb77078a92c03ff441f57edf69\", \"--exit-dir\", \"/var/run/crio/exits\", \"-l\", \"/var/log/pods/kube-system_calico-node-dxccr_2144dc4f-2713-4bc0-bd7b-7d523a061293/upgrade-ipam/22.log\", \"--log-level\", \"info\", \"-n\", \"k8s_upgrade-ipam_calico-node-dxccr_kube-system_2144dc4f-2713-4bc0-bd7b-7d523a061293_22\", \"-P\", \"/var/lib/containers/run/overlay-containers/53799e2601e9c7ff6f70489034f8a31887395acb77078a92c03ff441f57edf69/userdata/conmon-pidfile\", \"-p\", \"/var/lib/containers/run/overlay-containers/53799e2601e9c7ff6f70489034f8a31887395acb77078a92c03ff441f57edf69/userdata/pidfile\", \"--persist-dir\", \"/var/lib/containers/storage/overlay-containers/53799e2601e9c7ff6f70489034f8a31887395acb77078a92c03ff441f57edf69/userdata\", \"-r\", \"/usr/local/bin/lxcri\", \"--runtime-arg\", \"--root=/run/lxcri\", \"--socket-dir-path\", \"/var/run/crio\", \"-u\", \"53799e2601e9c7ff6f70489034f8a31887395acb77078a92c03ff441f57edf69\", \"-s\"}"}
*/
type conmon struct {
	syncPipe   int
	startPipe  int
	attachPipe int
	version    string

	// cmdline flags
	showVersion      bool
	bundlePath       string
	logFile          string
	containerID      string
	logLevel         string
	containerName    string
	exitDir          string
	pidFile          string
	pidFileContainer string

	runtime       string
	systemdCgroup bool
	runtimeArgs   string
	socketDirPath string
	containerUUID string
	persistDir    string

	attach bool

	//
	buf             *bytes.Buffer
	logFileInstance *os.File
	log             zerolog.Logger
}

var instance = conmon{
	syncPipe:   -1,
	startPipe:  -1,
	attachPipe: -1,
	version:    "2.0.22",
}

func (mon *conmon) parseEnv() (err error) {
	if val, ok := os.LookupEnv("_OCI_SYNCPIPE"); ok {
		mon.syncPipe, err = strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("failed to parse _OCI_SYNCPIPE value %q", val)
		}
	}
	if val, ok := os.LookupEnv("_OCI_STARTPIPE"); ok {
		mon.startPipe, err = strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("failed to parse _OCI_STARTPIPE value %q", val)
		}
	}

	if val, ok := os.LookupEnv("_OCI_ATTACHPIPE"); ok {
		mon.attachPipe, err = strconv.Atoi(val)
		if err != nil {
			return fmt.Errorf("failed to parse _OCI_ATTACHPIPE value %q", val)
		}
	}
	return nil
}

func main() {
	fs := flag.NewFlagSet("conmon", flag.ContinueOnError)
	fs.BoolVar(&instance.showVersion, "version", false, "show version")
	fs.StringVar(&instance.bundlePath, "b", "", "bundle directory")
	fs.StringVar(&instance.logFile, "l", "", "container process log file path")
	fs.StringVar(&instance.containerID, "c", "", "container ID")
	fs.StringVar(&instance.containerUUID, "u", "", "Container UUID")
	fs.StringVar(&instance.logLevel, "log-level", "", "log level")
	fs.StringVar(&instance.containerName, "n", "", "container name")
	fs.StringVar(&instance.exitDir, "exit-dir", "", "Path to the directory where exit files are written")
	fs.StringVar(&instance.pidFile, "P", "", "PID file for the conmon process")
	fs.StringVar(&instance.pidFileContainer, "p", "", "PID file for the initial pid inside of container")
	fs.StringVar(&instance.persistDir, "persist-dir", "", "Persistent directory for a container that can be used for storing container data")
	fs.StringVar(&instance.runtime, "r", "", "Path to runtime binary (must be lxcri)")
	fs.StringVar(&instance.runtimeArgs, "runtime-arg", "", "Runtime argument")
	fs.BoolVar(&instance.systemdCgroup, "s", false, "Enable systemd cgroup manager, rather then use the cgroupfs directly")
	fs.StringVar(&instance.socketDirPath, "socket-dir-path", "", "Location of container attach sockets")

	fs.BoolVar(&instance.attach, "exec-attach", false, "Attach to an exec session")

	errParse := fs.Parse(os.Args[1:])

	if instance.showVersion {
		fmt.Println("conmon version " + instance.version)
		os.Exit(0)
	}

	var err error
	instance.logFileInstance, err = log.OpenFile("/tmp/lxcri-conmon.log", 0640)
	if err != nil {
		panic(err)
	}
	defer instance.logFileInstance.Close()
	instance.log = log.NewLogger(instance.logFileInstance, log.DebugLevel).Logger()

	if errParse != nil {
		instance.log.Error().Msgf("failed to parse cmdline arguments: %s", err)
	}

	if err := instance.parseEnv(); err != nil {
		panic(err)
	}

	b := make([]byte, 8192)
	instance.buf = bytes.NewBuffer(b)

	instance.log.Debug().Msgf("%#v", os.Args)
	instance.log.Debug().Msgf("%#v", instance)

	if err := instance.syncStart(); err != nil {
		panic(err)
	}
}

func (c *conmon) syncStart() error {
	// handle start pipe
	if instance.startPipe == -1 {
		instance.log.Debug().Msg("startPipe is not defined")
		return nil
	}
	f := os.NewFile(uintptr(instance.startPipe), "start-pipe")
	// FIXME only close if c.attach is false
	defer f.Close()
	n, err := io.Copy(c.buf, f)
	if err != nil && err != io.EOF {
		return err
	}
	if n < 0 {
		return fmt.Errorf("start-pipe read failed")
	}
	c.log.Debug().Msg("startPipe sucessfully read")
	return nil
}

// user default
// lxcri --log-file ~/.cache/lxcri.log --container-log-file ~/.cache/lxcri.log --root ~/.cache/lxcri/run config --update-current
/*
var defaultApp = app{
	Runtime: lxcri.Runtime{
		Root:          "/run/lxcri",
		MonitorCgroup: "lxcri-monitor.slice",
		PayloadCgroup: "lxcri.slice",
		LibexecDir:    defaultLibexecDir,
		Features: lxcri.RuntimeFeatures{
			Apparmor:      true,
			Capabilities:  true,
			CgroupDevices: true,
			Seccomp:       true,
		},
	},
	LogConfig: logConfig{
		LogFile:           "/var/log/lxcri/lxcri.log",
		LogLevel:          "info",
		ContainerLogFile:  "/var/log/lxcri/lxcri.log",
		ContainerLogLevel: "warn",
	},

	Timeouts: timeouts{
		CreateTimeout: 60,
		StartTimeout:  30,
		KillTimeout:   10,
		DeleteTimeout: 10,
	},
}

var clxc = defaultApp

func (con *conmon) doCreate() error {
	if err := clxc.Init(); err != nil {
		return err
	}

	cfg := lxcri.ContainerConfig{
		ContainerID:   con.containerID,
		BundlePath:    con.bundlePath,
		ConsoleSocket: con.consoleSocket,
		SystemdCgroup: con.systemdCgroup,
		Log:           clxc.Runtime.Log,
		LogFile:       clxc.LogConfig.ContainerLogFile,
		LogLevel:      clxc.LogConfig.ContainerLogLevel,
	}

	specPath := filepath.Join(cfg.BundlePath, lxcri.BundleConfigFile)
	spec, err := specki.LoadSpecJSON(specPath)
	if err != nil {
		return fmt.Errorf("failed to load container spec from bundle: %w", err)
	}
	cfg.Spec = spec
	pidFile := ctxcli.String("pid-file")

	timeout := time.Duration(clxc.Timeouts.CreateTimeout) * time.Second
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	err = doCreateInternal(ctx, &cfg, pidFile)
	if err != nil {
		clxc.Log.Error().Msgf("failed to create container: %s", err)
		// Create a new context because create may fail with a timeout.
		ctx, cancel := context.WithTimeout(context.Background(), time.Duration(clxc.Timeouts.DeleteTimeout)*time.Second)
		defer cancel()
		if err := clxc.Delete(ctx, clxc.containerID, true); err != nil {
			clxc.Log.Error().Err(err).Msg("failed to destroy container")
		}
		return err
	}
	return nil
}

func doCreateInternal(ctx context.Context, cfg *lxcri.ContainerConfig, pidFile string) error {
	c, err := clxc.Create(ctx, cfg)
	if err != nil {
		return err
	}
	defer releaseContainer(c)

	if pidFile != "" {
		//err := createPidFile(pidFile, c.Pid)
		err := createPidFile(pidFile, c.LinuxContainer.InitPid())
		if err != nil {
			return err
		}
	}
	return nil
}

func releaseContainer(c *lxcri.Container) {
	if c == nil {
		return
	}
	if err := c.Release(); err != nil {
		app.Runtime.Log.Error().Msgf("failed to release container: %s", err)
	}
}
*/
