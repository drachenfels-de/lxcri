FROM docker.io/library/ubuntu:latest as build-base
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
#ARG LXC_CONFIGURE
RUN apt-get install -qq --no-install-recommends --yes \
libapparmor-dev libc6-dev libcap-dev libseccomp-dev
WORKDIR /tmp/build
COPY $LXC_SRC .
RUN tar -xf $(basename $LXC_SRC) --strip-components=1 --no-same-owner
RUN ./configure --enable-bash=no --enable-seccomp=yes \
--enable-capabilities=yes --enable-apparmor=yes \
--enable-tools=no --enable-commands=no \
--enable-examples=no --enable-static=yes \
--enable-doc=no --enable-api-docs=no \
${LXC_CONFIGURE}
# liblxc `make install` fails for static only build
RUN make install
#; exit 0
#RUN mkdir -p /usr/local/lib/pkgconfig && cp lxc.pc /usr/local/lib/pkgconfig
#RUN mkdir -p /usr/local/include/lxc && \
#cp src/lxc/attach_options.h src/lxc/lxccontainer.h src/lxc/version.h \
#/usr/local/include/lxc

FROM build-go AS lxcri
ARG LXCRI_SRC
ARG STATIC
ARG PREFIX
COPY --from=lxc /usr/local/ /usr/local/
RUN apt-get update
RUN apt-get install -qq --no-install-recommends --yes \
libapparmor-dev libcap-dev libseccomp-dev
WORKDIR /tmp/build
COPY $LXCRI_SRC .
RUN tar -xf $(basename $LXCRI_SRC) --strip-components=1
RUN STATIC=$STATIC make build
RUN PREFIX=$PREFIX make install
