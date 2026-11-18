#!/usr/bin/env bash
set -euo pipefail

WORKSHOP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME="operator-workshop"
KUBECONFIG_PATH="$WORKSHOP_DIR/kubeconfig.yaml"

echo "Setting up operator workshop environment"

echo "### KIND CLUSTER (1/2) ###"
if kind get clusters | grep -qx "$CLUSTER_NAME"; then
  echo "Cluster $CLUSTER_NAME already exists, skipping creation"
else
  kind create cluster --name "$CLUSTER_NAME" --wait 120s
fi
kind get kubeconfig --name "$CLUSTER_NAME" > "$KUBECONFIG_PATH"

echo "### NAMESPACE AND CREDENTIALS (2/2) ###"
export KUBECONFIG="$KUBECONFIG_PATH"
kubectl apply -f "$WORKSHOP_DIR/postgres.yaml"

echo "Done. Run: export KUBECONFIG=$KUBECONFIG_PATH"
