#!/usr/bin/env bash
set -euo pipefail

HACK_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(dirname "$HACK_DIR")"
KUBEBUILDER="kubebuilder"
BIN_PATH=$REPO_ROOT/$KUBEBUILDER

if [ ! -f "$BIN_PATH" ]; then
  curl -L -o $BIN_PATH "https://go.kubebuilder.io/dl/latest/$(go env GOOS)/$(go env GOARCH)"
  chmod +x $BIN_PATH
fi

cd $REPO_ROOT

./kubebuilder init --repo cinqict.nl/operators

