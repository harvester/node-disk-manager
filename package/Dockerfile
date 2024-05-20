# syntax=docker/dockerfile:1.7.0

FROM registry.suse.com/bci/bci-base:15.5

# util-linux-systemd -> for `lsblk` command
# e2fsprogs -> for `mkfs.ext4` command
# iproute2 -> for `ip` command
RUN zypper -n rm container-suseconnect && \
    zypper -n install util-linux-systemd e2fsprogs iproute2 && \
    zypper -n clean -a && rm -rf /tmp/* /var/tmp/* /usr/share/doc/packages/*

ARG TARGETPLATFORM

RUN if [ "$TARGETPLATFORM" != "linux/amd64" ] && [ "$TARGETPLATFORM" != "linux/arm64" ]; then \
    echo "Error: Unsupported TARGETPLATFORM: $TARGETPLATFORM" && \
    exit 1; \
    fi

ENV ARCH=${TARGETPLATFORM#linux/}

COPY bin/node-disk-manager-${ARCH} /usr/bin/node-disk-manager
CMD ["node-disk-manager"]
