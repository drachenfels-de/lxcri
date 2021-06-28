#!/bin/sh -eux

LXCRI_VERSION="${LXCRI_VERSION:-v0.12.1}"
DEV="${DEV:-}"

# if DEV environment variable is defined, then build lxcri from
# a tarball of the latest (local) commit.
if ! [ -z $DEV ]; then
	LXCRI_VERSION=$(git describe --always --tags --long)
fi

STATIC="${STATIC:-}"
if ! [ -z $STATIC ]; then
	LXCRI_VERSION="${LXCRI_VERSION}-static"
fi

BUILD_TAG=${BUILD_TAG:-github.com/lxc/lxcri:$LXCRI_VERSION}

c=$(buildah from ${BUILD_TAG})
m=$(buildah mount $c)
tar cf lxcri-${LXCRI_VERSION}.tar -C $m/usr/local lxcri
buildah unmount $c
buildah delete $c
xz lxcri-${LXCRI_VERSION}.tar

echo "lxcri-${LXCRI_VERSION}.tar.xz"
