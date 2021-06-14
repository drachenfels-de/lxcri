#!/bin/sh -eu

MAX_OPEN_FILES=${MAX_OPEN_FILES:-30}
ARGS=${@:---failfast --count 1 -v ./...}
uid=$(id -u)

## wait for XDG_RUNTIME_DIR creation
if [ $uid != 0 ]; then
	if ! [ -e "/run/user/$uid/bus" ]; then
		sudo loginctl enable-linger $USER
		while ! [ -e "/run/user/$uid/bus" ]; do
			sleep 0.5
		done
	fi
fi

export LIBEXEC_DIR=${LIBEXEC_DIR:-$PWD}

# must set the XDG_RUNTIME_DIR if user was switched with sudo|su
export XDG_RUNTIME_DIR=/run/user/$uid
echo "Using XDG_RUNTIME_DIR='$XDG_RUNTIME_DIR'"

# lower max open files to detect file descriptor leaks fast
echo "Setting open file descriptor limit to $MAX_OPEN_FILES"
ulimit -n $MAX_OPEN_FILES

# systemd-run starts the given command in a new and writable cgroup
systemd-run --user --scope go test $ARGS
