#!/bin/sh -eu

MAX_OPEN_FILES=${MAX_OPEN_FILES:-30}
ARGS=${@:---failfast --count 1 -v ./...}

## wait for XDG_RUNTIME_DIR creation
if [ $UID != 0 ]; then
	if ! [ -e "/run/user/$UID/bus" ]; then
		sudo loginctl enable-linger $USER
		while ! [ -e "/run/user/$UID/bus" ]; do
			sleep 0.5
		done
	fi
fi

cp lxcri-test /tmp

export LIBEXEC_DIR=${LIBEXEC_DIR:-$PWD}

# must set the XDG_RUNTIME_DIR if user was switched with sudo|su
export XDG_RUNTIME_DIR=/run/user/$UID
echo "Using XDG_RUNTIME_DIR='$XDG_RUNTIME_DIR'"

# lower max open files to detect file descriptor leaks fast
echo "Setting open file descriptor limit to $MAX_OPEN_FILES"
ulimit -n $MAX_OPEN_FILES

# systemd-run starts the given command in a new and writable cgroup
systemd-run --user --scope go test $ARGS
