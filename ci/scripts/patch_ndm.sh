#!/bin/bash -e

source helpers.sh

wait_ndm_ready
patch_ndm_auto_provision
wait_ndm_ready
