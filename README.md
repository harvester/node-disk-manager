node-disk-manager
========

Node Disk Manager helps to manage host disks, implementing disk partitioning and file system formatting.

## Building

`make`

This will build both amd64 and arm64 binaries, plus a container image
which will be named something like `harvester/node-disk-manager:dev`.

To build a container image and push it to your own repo on dockerhub, do this:

```sh
export REPO="your dockerhub username"
make
docker push $REPO/node-disk-manager:dev
```

## Running

The binaries for each architecture can be run directly for development or testing purposes:

```sh
./bin/node-disk-manager-amd64 --node-name "$(hostname -s)"
./bin/node-disk-manager-arm64 --node-name "$(hostname -s)"
```

## Chart

The chart definition is managed on a central repo `https://github.com/harvester/charts`. Changes need to be sent to it.

https://github.com/harvester/charts/tree/master/charts/harvester-node-disk-manager

For more information, see [Chart README] https://github.com/harvester/charts/blob/master/README.md.

### Example PRs adopting chart changes from the repo:

1. Add change against `master` branch - [#423](https://github.com/harvester/charts/pull/423)
1. Release the change by adding to `release` branch - [#427](https://github.com/harvester/charts/pull/427)
1. Integrate change in harvester build - [#9365](https://github.com/harvester/harvester/pull/9365)

## Features

- [x] Disk provisioning as Longhorn disks with a simple boolean.
- [x] Disk formatting if needed with a simple boolean.
- [x] Disk discovery, including existing block devices, and hot plugged disks.
- [x] Support multiple storage controller (IDE/SATA/SCSI/Virtio).
- [x] Support virtual disks (WWN on the disk is required for unique identification).
- [ ] Device mapper and LVM are not yet supported.
- [ ] The behaviour of multipath devices is undefined.

## Architecture

The **Node Disk Manager** (a.k.a. NDM) is a simple Kubernetes controller,
following the famous [controller pattern]. It leverages Rancher's [wrangler]
framework to construct a controller.

NDM is a single binary built with Golang and designed as a Kubernetes [DaemonSet].
You can find more information about how NDM is shipped with Harvester from this
[helm chart definition].

NDM has two main functionalities: disk discovery and disk provisioning. Each
is handled by dedicated components in this project. We'll discuss each topic
separately later. First, let us learn about the custom resource for NDM:
**blockdevices**.

### `blockdevices` Custom Resource

A `blockdevice` is a Kubernetes custom resource (CR) that represents a 
block device on a node. The `blockdevice` CR records lower-level block device
information from the operating system, for example, file system status, mount
point, and UUIDs. These details are all stored in `status.deviceStatus`.

The name of a `blockdevice` is a global identifier across nodes within the
whole cluster. At this moment, we recommend disk you want to provision to have
at least WWN on it. It helps the system to globally identify the `blockdevice`
resource and link to real block device of the operating system. For disks with
a WWN, the global identifier is a hash of the concatenation of the node name,
with the disk's WWN, Vendor, Model and Serial Number.

Besides its `name` field, the most important fields you need to know are
`spec.fileSystem.provisioned` and `spec.fileSystem.forceFormatted`. The former
implies that a user expects the block device to be provisioned as Longhorn disk
for further usage. And the latter just indicates that NDM would perform a disk
formatting if not yet done before.

### Disk Discovery

As a daemonset workload, each NDM instance takes charge of disks on its own node.
There are two components collecting the information of disks on the node, as
well as creating, updating, or deleting corresponding blockdevice CRs.

The first is `scanner`. It scans all supported block devices on the system and
creates a new `blockdevice` CR if one does not exist, or deletes the old CR if
is already removed from the system. For block devices that need to be updated, it
simply enqueues the `blockdevice` CR to let blockdevice controller handle the
update path to prevent any possible race condition. Scanner also periodically
scans the system to inform the controller to update info if needed.

The other key component is `udev`, which utilizes Linux's dynamic device 
management mechanism. `udev`, as a supplement of scanner, mostly behaves the same
as scanner, but instantly for responding to hot-plugged devices.

There is a module `filter`. It comprises several filter functions, which
have their own predicates to determine which block device should be collected by
scanner and udev.

### Disk Provisioning

The controller of NDM listens for changes of `blockdevice` CR and performs
corresponding actions, namely

- Format disk
- Mount/Unmount filesystem
- Provision/Unprovision disk to/from Longhorn
- Update device status details

Which actual action to perform are determined by the combination of
`spec.fileSystem`, device formatting and mounting status, and
`status.provisionPhase`. The last one indicates whether the block device is 
currently used by Longhorn.

To avoid any race condition, the controller must be the only component that 
updates existing `blockdevice` CR. Other components who need an update must 
enqueue the CR instead.

[controller pattern]: https://kubernetes.io/docs/concepts/architecture/controller/#controller-pattern
[wrangler]: https://github.com/rancher/wrangler/
[DaemonSet]: https://kubernetes.io/docs/concepts/workloads/controllers/daemonset/
[helm chart definition]: https://github.com/harvester/charts/tree/master/charts/harvester-node-disk-manager

## Appendix
We recommend user use the SCSI device, which contains the `WWN` to test the NDM.

Here we give the Sample XML for `libvirt` to create a SCSI device with `WWN`.

``` xml
    <disk type='file' device='disk'>
      <driver name='qemu' type='qcow2'/>
      <source file='/tmp/libvirt_disks/harvester_harvester-node-0-sda.qcow2'/>
      <target dev='sda' bus='scsi'/>
      <wwn>0x5000c50015ac3bd9</wwn>
    </disk>
```

**NOTE**: When disks don't have a WWN, NDM will use filesystem UUID as a unique identifier.
That has some limitations. For example, the UUID will be missing if the filesystem metadata is broken.

## License
Copyright (c) 2026 [SUSE, LLC.](https://www.suse.com/)

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

[http://www.apache.org/licenses/LICENSE-2.0](http://www.apache.org/licenses/LICENSE-2.0)

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
