FROM ubuntu:latest as build-base
RUN apt-get update
RUN apt-get install -yy build-essential

FROM ubuntu:latest as build-go
ARG GOLANG
WORKDIR /usr/local
ADD $GOLANG .
ENV PATH="/usr/local/go/bin:${PATH}"

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
#FROM build-base as crio
#ARG CRIO_SRC
#WORKDIR /tmp/build
#COPY $CRIO_SRC .
#RUN tar -xf $(basename $CONMON_SRC) --strip-components=1
#RUN make
#RUN make install

FROM build-base AS lxc
ARG LXC_SRC
RUN apt-get install -qq --no-install-recommends --yes \
    libapparmor-dev libbtrfs-dev libc6-dev libcap-dev \
    libdevmapper-dev libseccomp-dev
WORKDIR /tmp/build
COPY $LXC_SRC .
RUN tar -xf $(basename $LXC_SRC) --strip-components=1 --no-same-owner
RUN ./configure --enable-bash=no --enable-seccomp=yes \
    --enable-capabilities=yes --enable-apparmor=yes \
    --enable-tools=no --enable-commands=no \
    --enable-static=no --enable-examples=no \
    --enable-doc=no --enable-api-docs=no
RUN make install


FROM build-go AS lxcri
ARG LXCRI_SRC
COPY --from=lxc /usr/local/ /usr/local/

# go-lxc requires libseccomp
RUN apt-get update
RUN apt-get install -qq --no-install-recommends --yes \
    libapparmor1 libbtrfs0 libcap2 libdevmapper1.02.1 libseccomp2

WORKDIR /tmp/build
COPY $LXCRI_SRC .
RUN tar -xf $(basename $LXCRI_SRC) --strip-components=1
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig
RUN make install

#FROM ubuntu:latest
#ARG CNI_PLUGIN_DIR
#ARG CONMON_BIN
#ARG CRICTL_BIN
#
##COPY $CONMON_BIN /usr/local/bin/conmon
##COPY --from=cni-plugins /tmp/build/bin/ $CNI_PLUGIN_DIR
##COPY --from=crio /usr/local/ /usr/local/
## Modify systemd service file to run with full privileges.
## This is required for the runtime to set cgroupv2 device controller eBPF.
##RUN sed -i 's/ExecStart=\//ExecStart=+\//' /usr/local/lib/systemd/system/crio.service
#COPY --from=lxc /usr/local/ /usr/local/
#RUN echo /usr/local >> /etc/ld.so.conf.d/local.conf && ldconfig
#COPY --from=lxcri /usr/local/ /usr/local/
#
#RUN apt-get purge -qq --yes $@
#RUN apt-get autoremove -qq --yes
#RUN apt-get clean -qq
#RUN rm -rf /var/lib/apt/lists/*

