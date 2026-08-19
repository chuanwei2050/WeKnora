#!/usr/bin/env bash
set -eu
export PATH=/usr/local/go/bin:$PATH
export GOMODCACHE=/go/pkg/mod
export GOFLAGS=-mod=mod
go test ./internal/application/service/chat_pipeline/ -run 'Explainability|GraphSchema|GraphSkip' -count=1
