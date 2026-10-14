#!/usr/bin/env bash
set -euo pipefail

echo "Setting up operator workshop environment"
echo "### KUBEBUILDER (1/) ###"

WORKSHOP_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$WORKSHOP_DIR")"
KUBEBUILDER="kubebuilder"
BIN_PATH=$REPO_ROOT/$KUBEBUILDER

if [ ! -f "$BIN_PATH" ]; then
  echo "Downloading kubebuilder bin"
  curl -L -o $BIN_PATH "https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)"
  chmod +x $BIN_PATH
  echo "Downloaded kubebuilder bin"
fi

cd $REPO_ROOT

if [ ! -f "./go.sum" ]; then
  ./kubebuilder init --repo cinqict.nl/operators
  rm -rf ./kubebuilder
else
  echo "Project is already initialized, skipping step"
fi

echo "### KIND CLUSTER (2/) ###"
docker compose -f "$WORKSHOP_DIR/docker-compose.yaml" run --rm kind

echo "### DEPLOYING DB ###"
export KUBECONFIG="$WORKSHOP_DIR/kubeconfig.yaml"

echo "Waiting for the cluster to be ready"
for i in $(seq 1 60); do
  if kubectl get nodes >/dev/null 2>&1; then
    break
  fi
  echo "Retrying ($i/60)"
  sleep 2
done

kubectl apply -f "$WORKSHOP_DIR/postgres.yaml"
