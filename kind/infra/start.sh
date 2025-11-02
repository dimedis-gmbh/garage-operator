#!/bin/bash
set -e

docker start $KIND_CLUSTER_NAME-control-plane $KIND_CLUSTER_NAME-worker $KIND_CLUSTER_NAME-worker2 $KIND_CLUSTER_NAME-worker3
