# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

See [AGENTS.md](./AGENTS.md) for detailed guidance.

## Quick Reference

```bash
# Build
go build ./...

# Test (requires DynamoDB local on port 8000)
go test -race ./... -timeout 30s

# Lint
golangci-lint run

# Local development
docker compose up -d dynamodb-local
go run cmd/gdnotify/main.go --storage-auto-create --storage-dynamo-db-endpoint=http://localhost:8000
```
