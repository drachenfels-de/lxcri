package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"

	lxc "gopkg.in/lxc/go-lxc.v2"
)

var seccompAction = map[specs.LinuxSeccompAction]string{
	specs.ActKill:  "kill",
	specs.ActTrap:  "trap",
	specs.ActErrno: "errno",
	specs.ActAllow: "allow",
	//specs.ActTrace: "trace",
	//specs.ActLog: "log",
	//specs.ActKillProcess: "kill_process",
}

func writeSeccompSyscall(w *bufio.Writer, sc specs.LinuxSyscall) error {
	for _, name := range sc.Names {
		action, ok := seccompAction[sc.Action]
		if !ok {
			return fmt.Errorf("unsupported seccomp action: %s", sc.Action)
		}
		if len(sc.Args) == 0 {
			fmt.Fprintf(w, "%s %s\n", name, action)
		} else {
			// Only write a single argument per line - this is required when the same arg.Index is used multiple times.
			// from `man 7 seccomp_rule_add_exact_array`
			// "When adding syscall argument comparisons to the filter it is important to remember
			// that while it is possible to have multiple comparisons in a single rule,
			// you can only compare each argument once in a single rule.
			// In other words, you can not have multiple comparisons of the 3rd syscall argument in a single rule."
			for _, arg := range sc.Args {
				fmt.Fprintf(w, "%s %s [%d,%d,%s,%d]\n", name, action, arg.Index, arg.Value, arg.Op, arg.ValueTwo)
			}
		}
	}
	return nil
}

func defaultAction(seccomp *specs.LinuxSeccomp) (string, error) {
	switch seccomp.DefaultAction {
	case specs.ActKill:
		return "kill", nil
	case specs.ActTrap:
		return "trap", nil
	case specs.ActErrno:
		return "errno 0", nil
	case specs.ActAllow:
		return "allow", nil
	case specs.ActTrace, specs.ActLog: // Not (yet) supported by lxc
		log.Warn().Str("action:", string(seccomp.DefaultAction)).Msg("unsupported seccomp default action")
		fallthrough
	//case specs.ActKillProcess: fallthrough // specs > 1.0.2
	default:
		return "kill", fmt.Errorf("Unsupported seccomp default action %q", seccomp.DefaultAction)
	}
}

func seccompArchs(seccomp *specs.LinuxSeccomp) ([]string, error) {
	var uts unix.Utsname
	if err := unix.Uname(&uts); err != nil {
		return nil, err
	}
	nativeArch := nullTerminatedString(uts.Machine[:])
	archs := make([]string, len(seccomp.Architectures))
	for _, a := range seccomp.Architectures {
		s := strings.ToLower(strings.TrimLeft(string(a), "SCMP_ARCH_"))
		if strings.ToLower(nativeArch) == s {
			// lxc seccomp code automatically adds syscalls to compat architectures
			return []string{nativeArch}, nil
		}
		archs = append(archs, s)
	}
	return archs, nil
}

func writeSeccompProfile(profilePath string, seccomp *specs.LinuxSeccomp) error {
	profile, err := os.OpenFile(profilePath, os.O_WRONLY|os.O_TRUNC|os.O_CREATE, 0440)
	if err != nil {
		return err
	}
	defer profile.Close()

	w := bufio.NewWriter(profile)
	defer w.Flush()

	w.WriteString("2\n")
	action, err := defaultAction(seccomp)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "allowlist %s\n", action)

	platformArchs, err := seccompArchs(seccomp)
	if err != nil {
		return errors.Wrap(err, "Failed to detect platform architecture")
	}
	log.Debug().Str("action:", action).Strs("archs:", platformArchs).Msg("create seccomp profile")
	for _, arch := range platformArchs {
		fmt.Fprintf(w, "[%s]\n", arch)
		for _, sc := range seccomp.Syscalls {
			if err := writeSeccompSyscall(w, sc); err != nil {
				return err
			}
		}
	}
	return nil
}

func configureSeccomp(c *lxc.Container, spec *specs.Spec) error {
	if !clxc.Seccomp {
		return nil
	}

	if spec.Linux.Seccomp == nil || len(spec.Linux.Seccomp.Syscalls) == 0 {
		return nil
	}

	// TODO warn if seccomp is not available in liblxc

	if spec.Process.NoNewPrivileges {
		if err := clxc.SetConfigItem("lxc.no_new_privs", "1"); err != nil {
			return err
		}
	}

	profilePath := clxc.RuntimePath("seccomp.conf")
	if err := writeSeccompProfile(profilePath, spec.Linux.Seccomp); err != nil {
		return err
	}

	return clxc.SetConfigItem("lxc.seccomp.profile", profilePath)
}
