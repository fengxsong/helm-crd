#!/bin/sh

set -e

mydir=${0#*/}

vendor/k8s.io/code-generator/generate-groups.sh \
    all \
    github.com/fengxsong/helm-crd/pkg/client \
    github.com/fengxsong/helm-crd/pkg/apis \
    helm.bitnami.com:v1
