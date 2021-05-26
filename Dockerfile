FROM ubuntu:latest as build-base
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update
RUN apt-get install -qq build-essential ca-certificates pkg-config


FROM build-base as build-go
ARG GOLANG
WORKDIR /opt
ADD $GOLANG .
ENV PATH="/opt/go/bin:${PATH}"


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
RUN apt-get update
RUN apt-get install -qq --no-install-recommends --yes \
    libapparmor1 libbtrfs0 libcap2 libdevmapper1.02.1 libseccomp2
WORKDIR /tmp/build
COPY $LXCRI_SRC .
RUN tar -xf $(basename $LXCRI_SRC) --strip-components=1
ENV PKG_CONFIG_PATH=/usr/local/lib/pkgconfig
RUN make install


FROM build-go as crio
ARG CRIO_SRC
WORKDIR /tmp/build
COPY $CRIO_SRC .
RUN tar -xf $(basename $CRIO_SRC) --strip-components=1
RUN make
RUN make install

FROM ubuntu:latest
ARG CNI_PLUGIN_DIR
ARG CNI_PLUGINS
ARG CONMON
ARG CRICTL
RUN apt-get update
RUN apt-get install -qq --no-install-recommends --yes \
    libapparmor1 libbtrfs0 libcap2 libdevmapper1.02.1 libseccomp2
COPY $CONMON /usr/local/bin/conmon
ADD $CNI_PLUGINS $CNI_PLUGIN_DIR
COPY --from=crio /etc/crio /etc/crictl.yaml /etc
COPY --from=crio /usr/local/ /usr/local/
## Modify systemd service file to run with full privileges.
## This is required for the runtime to set cgroupv2 device controller eBPF.
RUN sed -i 's/ExecStart=\//ExecStart=+\//' /usr/local/lib/systemd/system/crio.service
# configure crio to use lxcri
RUN crio config
COPY --from=lxc /usr/local/ /usr/local/
RUN echo /usr/local >> /etc/ld.so.conf.d/local.conf && ldconfig
COPY --from=lxcri /usr/local/ /usr/local/
RUN lxcri config --update-current
RUN apt-get clean -qq
RUN rm -rf /var/lib/apt/lists/*
