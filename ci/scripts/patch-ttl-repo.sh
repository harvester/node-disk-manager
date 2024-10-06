#!/bin/bash -e

COMMIT=$(git rev-parse --short HEAD)
IMAGE=ttl.sh/node-disk-manager-${COMMIT}
IMAGE_WEBHOOK=ttl.sh/node-disk-manager-webhook-${COMMIT}

yq e -i ".image.repository = \"${IMAGE}\"" ci/charts/ndm-override.yaml
yq e -i ".image.tag = \"1h\"" ci/charts/ndm-override.yaml 
yq e -i ".webhook.image.repository = \"${IMAGE_WEBHOOK}\"" ci/charts/ndm-override.yaml
yq e -i ".webhook.image.tag = \"1h\"" ci/charts/ndm-override.yaml 