FROM docker.io/library/ubuntu:latest as build-base
ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update \
&& apt-get install -qq --no-install-recommends --yes \
build-essential ca-certificates pkg-config \
libapparmor-dev libcap-dev libseccomp-dev \
&& apt-get clean -qq \
&& rm -rf /var/lib/apt/lists/*

FROM build-base as lxc
ARG LXC_SRC
WORKDIR /tmp/build
COPY $LXC_SRC .
RUN tar -xf $(basename $LXC_SRC) --strip-components=1 --no-same-owner
RUN ./configure --enable-bash=no --enable-seccomp=yes \
--enable-capabilities=yes --enable-apparmor=yes \
--enable-tools=no --enable-commands=no \
--enable-examples=no --enable-static=yes \
--enable-doc=no --enable-api-docs=no
RUN make install

FROM build-base as build-golang
ARG GOLANG
WORKDIR /opt
ADD $GOLANG .
ENV PATH="/opt/go/bin:${PATH}"

FROM build-golang
ARG LXCRI_SRC
ARG STATIC
ARG PREFIX
COPY --from=lxc /usr/local/ /usr/local/
WORKDIR /tmp/build
COPY $LXCRI_SRC .
RUN tar -xf $(basename $LXCRI_SRC) --strip-components=1
RUN STATIC=$STATIC make build
RUN PREFIX=$PREFIX make install
