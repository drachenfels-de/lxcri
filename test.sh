#!/bin/sh

export LXCRI_LIBEXEC=$(pwd)
# must set the XDG_RUNTIME_DIR if user was switched with sudo|su
export XDG_RUNTIME_DIR=/run/user/$UID
# lower max open files to detect file descriptor leaks fast
ulimit -n 30
# systemd-run starts the given command in a new and writable cgroup
systemd-run --user --scope go test $@
