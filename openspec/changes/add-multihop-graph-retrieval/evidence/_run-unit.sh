#!/usr/bin/env bash
set -eu
export PATH=/usr/local/go/bin:$PATH
export GOMODCACHE=/go/pkg/mod
export GOFLAGS=-mod=mod
go test ./internal/types/ -run 'Graph|SearchPath|Canonical' -count=1
echo "---"
echo "NEO4J_TEST_FILES:"
ls -1 internal/application/repository/retriever/neo4j/*_test.go 2>/dev/null || echo "(none ? neo4j package lacks *_test.go)"
