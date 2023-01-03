#!/bin/bash -e

COMMIT=$(git rev-parse --short HEAD)
IMAGE=ttl.sh/node-disk-manager-${COMMIT}

yq e -i ".image.repository = \"${IMAGE}\"" ci/charts/ndm-override.yaml
yq e -i ".image.tag = \"1h\"" ci/charts/ndm-override.yaml 