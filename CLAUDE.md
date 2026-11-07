# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

nprobe is a modern replacement for smokeping, built in Go. It's a distributed network monitoring system with a head node and satellite architecture for latency/RTT monitoring.

## Architecture

- **Head Node**: Central server that manages configuration, receives data from satellites, and stores metrics in InfluxDB
- **Satellites**: Remote probes that connect to the head node, receive target assignments, and perform actual network tests
- **Targets**: Hosts/endpoints to be monitored (supports ICMP ping and HTTP probes)

Key components:
- `main.go`: Contains both head node HTTP API server and satellite client logic
- `satellite.go`: Probe execution logic (ICMP/HTTP) and data submission
- `util.go`: Utility functions (random string generation)

## Development Commands

### Build
```bash
make build
```

### Run Head Node (Server)
```bash
make run-server
# Uses config/config-no-db.json and runs in debug mode
```

### Run Satellite (Client)
```bash
make run-client
# Connects to localhost head node with secret authentication
```

### Docker Build
```bash
make docker
```

### Clean Build Artifacts
```bash
make clean
```

## Configuration

- Configuration files are JSON format in `config/` directory
- `config/config.json.example` provides template
- Head nodes use config file specified by `-config` flag (default: `config/config.json`)
- Satellites authenticate using `NPROBE_SECRET` environment variable

## Key Architecture Notes

- The application runs in two modes: "head" or "satellite" (default)
- Head nodes serve HTTP API on configurable port (default 8000)
- Satellites connect to head via HTTPS/HTTP and receive target assignments
- All HTTP communication uses custom headers for versioning and authentication
- ICMP probes require elevated privileges (`--privileged` flag and root access)
- Configuration versioning system forces satellite restarts when head config changes
- Concurrent worker goroutines handle multiple target monitoring on satellites

## API Structure

- Authentication via `X-Authorization` header
- Public endpoints: `/healthz`, `/satellites/{name}`, `/satellites/{name}/targets`, `/version`
- Private endpoints (require auth): `/config`, satellite CRUD operations
- Metrics submission: `PUT /satellites/{name}/{target}/metrics`

## Dependencies

Built with Go 1.23+, key dependencies:
- `github.com/go-chi/chi/v5` (HTTP router)
- `github.com/prometheus-community/pro-bing` (ICMP ping)
- `github.com/influxdata/influxdb-client-go/v2` (metrics storage)
- `github.com/spf13/viper` (configuration)
- `github.com/sirupsen/logrus` (logging)