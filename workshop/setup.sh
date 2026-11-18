#!/usr/bin/env bash
set -euo pipefail

WORKSHOP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CLUSTER_NAME="operator-workshop"
KUBECONFIG_PATH="$WORKSHOP_DIR/kubeconfig.yaml"

echo "Setting up operator workshop environment"

echo "### KIND CLUSTER (1/2) ###"
docker compose -f "$WORKSHOP_DIR/docker-compose.yaml" run --rm kind

echo "### NAMESPACE AND CREDENTIALS (2/2) ###"
export KUBECONFIG="$KUBECONFIG_PATH"
kubectl apply -f "$WORKSHOP_DIR/postgres.yaml"

echo "Done. Run: export KUBECONFIG=$KUBECONFIG_PATH"
