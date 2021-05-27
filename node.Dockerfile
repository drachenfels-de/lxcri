# https://github.com/containers/conmon/archive/refs/tags/v2.0.27.tar.gz
# https://github.com/containers/conmon/releases/download/v2.0.27/conmon.amd64
#FROM build-base AS conmon
#ARG CONMON_SRC
#WORKDIR /tmp/build
#COPY $CONMON_SRC .
#RUN tar -xf $(basename $CONMON_SRC) --strip-components=1
#RUN make

# https://github.com/containernetworking/plugins/releases/download/v0.9.1/cni-plugins-linux-amd64-v0.9.1.tgz
# https://github.com/containernetworking/plugins/releases/download/v0.9.1/cni-plugins-linux-amd64-v0.9.1.tgz.sha256
#FROM golang:latest as cni-plugins
#ARG CNI_PLUGINS_SRC
#WORKDIR /tmp/build
#COPY $CNI_PLUGINS_SRC .
#RUN tar -xf $(basename $CNI_SRC) --strip-components=1
#RUN ./build_linux.sh


# https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.21.0/crictl-v1.21.0-linux-amd64.tar.gz
# https://github.com/kubernetes-sigs/cri-tools/releases/download/v1.21.0/crictl-v1.21.0-linux-amd64.tar.gz.sha256

# https://github.com/cri-o/cri-o/archive/refs/tags/v1.20.2.tar.gz
