#!/bin/sh -eux

LXC_SRC="lxc-4.0.9.tar.gz"
LXC_SRC_URL="https://linuxcontainers.org/downloads/lxc/$LXC_SRC"
LXC_SRC_SUM="1fcf0610e9140eceb4be2334eb537bb9c5a213faea77c793ab3c62b86f37e52b"

LXCRI_VERSION="v0.12.1"
LXCRI_SRC="lxcri-${LXCRI_VERSION}.tar.gz"
LXCRI_SRC_URL="https://github.com/lxc/lxcri/archive/refs/tags/${LXCRI_VERSION}.tar.gz"
LXCRI_SRC_SUM="35943570d88f8c0fdacdaa62b01b111e507594fd3155de5df39fdaa94e17c13c"

GOLANG="go1.16.5.linux-amd64.tar.gz"
GOLANG_URL="https://golang.org/dl/$GOLANG"
GOLANG_SUM="b12c23023b68de22f74c0524f10b753e7b08b1504cb7e417eccebdd3fae49061"

DL=downloads

download() {
	local src=$1
	local url=$2
	local sum=$3

	[ -d $DL ] || mkdir $DL

	if ! [ -f "$DL/$src" ]; then
		echo "Downloading $url"
		wget --quiet $url -O $DL/$src
		if ! (echo "$sum  $DL/$src" | sha256sum -c); then
			rm "$DL/$src"
			return 1
		fi
	fi
}

DEV="${DEV:-}"

# if DEV environment variable is defined, then build lxcri from
# a tarball of the latest (local) commit.
if ! [ -z $DEV ]; then
	LXCRI_SRC=lxcri-master.tar.gz
	LXCRI_VERSION=$(git describe --always --tags --long)
	git archive --prefix lxcri-master/ -o $DL/$LXCRI_SRC HEAD
fi

STATIC="${STATIC:-}"
if ! [ -z $STATIC ]; then
	LXCRI_VERSION="${LXCRI_VERSION}-static"
fi

BUILD_TAG=${BUILD_TAG:-github.com/lxc/lxcri:$LXCRI_VERSION}
BUILD_CMD=${BUILD_CMD:-buildah bud}

build() {
	download $LXC_SRC $LXC_SRC_URL $LXC_SRC_SUM
	download $GOLANG $GOLANG_URL $GOLANG_SUM
	download $LXCRI_SRC $LXCRI_SRC_URL $LXCRI_SRC_SUM

	$BUILD_CMD $@ \
		--build-arg LXC_SRC="$DL/$LXC_SRC" \
		--build-arg LXCRI_SRC="$DL/$LXCRI_SRC" \
		--build-arg PREFIX="/usr/local/lxcri" \
		--build-arg STATIC="$STATIC" \
		--build-arg GOLANG="$DL/$GOLANG" \
		--tag "$BUILD_TAG"
}

publish() {
	c=$(buildah from ${BUILD_TAG})
	m=$(buildah mount $c)
	tar cf lxcri-${LXCRI_VERSION}.tar -C $m/usr/local lxcri
	buildah unmount $c
	buildah delete $c
	xz lxcri-${LXCRI_VERSION}.tar

	echo "lxcri-${LXCRI_VERSION}.tar.xz"
}

CMD=${1:-build}
case $CMD in
"" | "build")
	build ${@:2}
	;;
"publish")
	publish ${@:2}
	;;
*)
	echo "no such command: $1" && exit 1
	;;
esac
