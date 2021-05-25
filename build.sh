#!/bin/sh -eux

# https://github.com/containers/conmon/archive/refs/tags/v2.0.27.tar.gz
# https://github.com/containers/conmon/releases/download/v2.0.27/conmon.amd64
#FROM build-base AS conmon
#ARG CONMON_SRC
#RUN mkdir /tmp/build
#WORKDIR /tmp/build
#COPY $CONMON_SRC .
#RUN tar -xf $(basename $CONMON_SRC) --strip-components=1
#RUN make

# https://github.com/containernetworking/plugins/releases/download/v0.9.1/cni-plugins-linux-amd64-v0.9.1.tgz
# https://github.com/containernetworking/plugins/releases/download/v0.9.1/cni-plugins-linux-amd64-v0.9.1.tgz.sha256
#FROM golang:latest as cni-plugins
#ARG CNI_PLUGINS_SRC
#RUN mkdir /tmp/build
#WORKDIR /tmp/build
#COPY $CNI_PLUGINS_SRC .
#RUN tar -xf $(basename $CNI_SRC) --strip-components=1
#RUN ./build_linux.sh

# https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.21.0/crictl-v1.21.0-linux-amd64.tar.gz
# https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.21.0/crictl-v1.21.0-linux-amd64.tar.gz.sha256

# https://github.com/cri-o/cri-o/archive/refs/tags/v1.20.2.tar.gz
#FROM build-base as crio
#ARG CRIO_SRC
#RUN mkdir /tmp/build
#WORKDIR /tmp/build
#COPY $CRIO_SRC .
#RUN tar -xf $(basename $CONMON_SRC) --strip-components=1
#RUN make
#RUN make install
#!/bin/sh

# https://github.com/containers/conmon/archive/refs/tags/v2.0.27.tar.gz
# https://github.com/containers/conmon/releases/download/v2.0.27/conmon.amd64
CONMON="conmon.amd64"
CONMON_URL="https://github.com/containers/conmon/releases/download/v2.0.27/$CONMON"
CONMON_SUM="8d4048c4b84ae44c11c2604e5e5a296fbb7ff567a0e3433ce5dfdfd72d2506e1"

CNI_PLUGINS_VERSION="v0.9.1"
CNI_PLUGINS="cni-plugins-linux-amd64-${CNI_PLUGINS_VERSION}.tgz"
CNI_PLUGINS_URL="https://github.com/containernetworking/plugins/releases/download/${CNI_PLUGINS_VERSION}/$CNI_PLUGINS"
# https://github.com/containernetworking/plugins/releases/download/v0.9.1/cni-plugins-linux-amd64-v0.9.1.tgz.sha256
CNI_PLUGINS_SUM="962100bbc4baeaaa5748cdbfce941f756b1531c2eadb290129401498bfac21e7"

CRIO_VERSION="v1.20.2"
CRIO_SRC="crio-$CRIO_VERSION.tar.gz"
CRIO_SRC_URL="https://github.com/cri-o/cri-o/archive/refs/tags/$CRIO_VERSION.tar.gz"
CRIO_SRC_SUM="1c01d4a76cdcfe3ac24147eb1d5f6ebd782bd98fb0ac0c19b79bd5a6560b1481"
#CRIO_GIT_REPO=https://github.com/cri-o/cri-o.git
#CRIO_GIT_VERSION=v1.20.2

CRICTL_VERSION="v1.20.0"
CRICTL="crictl-${CRICTL_VERSION}-linux-amd64.tar.gz"
CRICTL_URL="https://github.com/kubernetes-sigs/cri-tools/releases/download/${CRICTL_VERSION}/${CRICTL}"
CRICTL_SUM="44d5f550ef3f41f9b53155906e0229ffdbee4b19452b4df540265e29572b899c"

# https://github.com/kubernetes/kubernetes/blob/master/CHANGELOG/CHANGELOG-1.20.md
#K8S_CHECKSUM=ac936e05aef7bb887a5fb57d50f8c384ee395b5f34c85e5c0effd8709db042359f63247d4a6ae2c0831fe019cd3029465377117e42fff1b00a8e4b7473b88db9
#K8S_URL="https://dl.k8s.io/v1.20.6/kubernetes-server-linux-amd64.tar.gz"

LXC_SRC="lxc-4.0.9.tar.gz"
LXC_SRC_URL="https://linuxcontainers.org/downloads/lxc/$LXC_SRC"
LXC_SRC_SUM="1fcf0610e9140eceb4be2334eb537bb9c5a213faea77c793ab3c62b86f37e52b"

# NOTE use https://github.com/lxc/lxcri/tarball/main for development ... (strip components)
LXCRI_SRC_DEV_URL="https://github.com/lxc/lxcri/tarball/main"
LXCRI_VERSION="v0.12.1"
LXCRI_SRC="lxcri-${LXCRI_VERSION}.tar.gz"
LXCRI_SRC_URL="https://github.com/lxc/lxcri/archive/refs/tags/${LXCRI_VERSION}.tar.gz"
LXCRI_SRC_SUM="35943570d88f8c0fdacdaa62b01b111e507594fd3155de5df39fdaa94e17c13c"

GOLANG="go1.16.4.linux-amd64.tar.gz"
GOLANG_URL="https://golang.org/dl/$GOLANG"
GOLANG_SUM="7154e88f5a8047aad4b80ebace58a059e36e7e2e4eb3b383127a28c711b4ff59"

DL=downloads
download() {
	local src=$1
	local url=$2
	local sum=$3

	if ! [ -f "$DL/$src" ]; then
		echo "Downloading $url"
		wget --quiet $url -O $DL/$src
		if ! (echo "$sum  $DL/$src" | sha256sum -c); then
			rm "$DL/$src"
			return 1
		fi
	fi
}

[ -d $DL ] || mkdir $DL
download $CONMON $CONMON_URL $CONMON_SUM
download $CNI_PLUGINS $CNI_PLUGINS_URL $CNI_PLUGINS_SUM
download $CRIO_SRC $CRIO_SRC_URL $CRIO_SRC_SUM
download $CRICTL $CRICTL_URL $CRICTL_SUM
download $LXC_SRC $LXC_SRC_URL $LXC_SRC_SUM
download $GOLANG $GOLANG_URL $GOLANG_SUM
#download $LXCRI_SRC $LXCRI_SRC_URL $LXCRI_SRC_SUM
LXCRI_SRC=lxcri-master.tar.gz
git archive --prefix lxcri-master/ -o $DL/$LXCRI_SRC HEAD

BUILD_CMD=${BUILD_CMD:-buildah bud}
$BUILD_CMD $@ \
	--build-arg CONMON=$DL/$CONMON \
	--build-arg CNI_PLUGINS=$DL/$CNI_PLUGINS \
	--build-arg CNI_PLUGIN_DIR=/usr/local/cni/plugins \
	--build-arg CRIO_SRC=$DL/$CRIO_SRC \
	--build-arg CRICTL=$DL/$CRICTL \
	--build-arg LXC_SRC=$DL/$LXC_SRC \
	--build-arg LXCRI_SRC=$DL/$LXCRI_SRC \
	--build-arg GOLANG=$DL/$GOLANG

add_kubernetes() {
	local checksum=$K8S_CHECKSUM
	local url=$K8S_URL
	local archive=$(basename $K8S_URL)

	cd ${TMPDIR}
	wget --quiet $url
	echo "$checksum  $archive" | sha512sum -c
	tar -x -z -f $archive -C $INSTALL_PREFIX/bin --strip-components=3 \
		kubernetes/server/bin/kubectl kubernetes/server/bin/kubeadm kubernetes/server/bin/kubelet
	rm $archive
}
