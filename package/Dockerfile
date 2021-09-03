FROM alpine
# parted -> for `parted` command
# util-linux -> for `lsblk` command
# e2fsprogs -> for `mkfs.ext4` command
RUN apk update && apk add -u --no-cache parted util-linux e2fsprogs
COPY bin/node-disk-manager /usr/bin/
CMD ["node-disk-manager"]
