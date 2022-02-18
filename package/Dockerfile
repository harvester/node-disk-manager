FROM registry.suse.com/bci/bci-base:15.3

# util-linux-systemd -> for `lsblk` command
# e2fsprogs -> for `mkfs.ext4` command
# iproute2 -> for `ip` command
RUN zypper -n rm container-suseconnect && \
    zypper -n install util-linux-systemd e2fsprogs iproute2 && \
    zypper -n clean -a && rm -rf /tmp/* /var/tmp/* /usr/share/doc/packages/*

COPY bin/node-disk-manager /usr/bin/
CMD ["node-disk-manager"]
