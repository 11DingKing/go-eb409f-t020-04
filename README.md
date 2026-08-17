# microgrid-ops

Work-order coordination backend for daily inspection and maintenance of energy
storage battery cabins in the Ejina Banner green microgrid.

## Purpose

Manages the full lifecycle of inspection and maintenance work orders across
battery cabins with strict concurrency control, state-machine-driven workflows,
dual-signature spare-parts requisitions, and failure-recovery automation:

- Inspectors detect anomalies (temperature over-limit, insulation alarm, cooling
  fan stop) and must report within 10 minutes; the cabin's grid-connection
  qualification is frozen immediately.
- Maintenance engineers require dispatch-chief authorization and dual-person
  badge verification before entering a high-voltage cabin.
- One cabin cannot hold an inspection and a maintenance order simultaneously;
  the later order queues and auto-advances when the first closes.
- Spare-parts requisitions require engineer + warehouse-manager dual sign;
  out-of-stock triggers emergency procurement.
- Restoration requires mandatory black-start and off-grid rechecks before the
  dispatch chief approves grid reconnection.
- Maintenance timeouts auto-escalate to the dispatch chief; approval
  interruptions fall back to pending-recheck; parts failures roll back inventory
  and requeue the order.

## Quick start

```bash
# Run directly
go run ./cmd/server

# Build and run
go build -o microgrid-ops ./cmd/server
./microgrid-ops
```

The server listens on **port 58295**.

## Docker

```bash
# Build (supports amd64 and arm64)
docker build -t microgrid-ops .

# Run
docker run -p 58295:58295 microgrid-ops
```

## Configuration

Edit `config.json` or override via environment variables:

| Key | Env | Default | Description |
|-----|-----|---------|-------------|
| port | `PORT` | 58295 | HTTP listen port |
| store_path | `STORE_PATH` | data/state.json | Persistence file |
| maintenance_timeout | `MAINTENANCE_TIMEOUT` | 5m | Response timeout before escalation |
| anomaly_report_deadline | `ANOMALY_REPORT_DEADLINE` | 10m | Max time to report an anomaly |
| scheduler_interval | — | 2s | Background poll interval |

## Main API endpoints

### Cabins
- `POST /api/cabins` — register a battery cabin
- `GET /api/cabins` — list all cabins
- `GET /api/cabins/{id}` — get cabin detail
- `PUT /api/cabins/{id}/conditions` — set sensor readings

### Work Orders
- `POST /api/workorders/inspection` — create inspection order
- `POST /api/workorders/maintenance` — create maintenance order (2 engineer IDs)
- `GET /api/workorders` — list all orders
- `GET /api/workorders/{id}` — get order detail
- `POST /api/workorders/{id}/detect-anomaly` — detect anomaly (freezes cabin)
- `POST /api/workorders/{id}/report-anomaly` — submit anomaly report
- `POST /api/workorders/{id}/authorize` — chief authorizes maintenance
- `POST /api/workorders/{id}/enter` — engineers enter cabin (dual-person)
- `POST /api/workorders/{id}/request-parts` — submit parts requisition
- `POST /api/workorders/{id}/confirm-parts` — manager dual-signs requisition
- `POST /api/workorders/{id}/emergency-procurement` — emergency stock supply
- `POST /api/workorders/{id}/complete` — mark repair complete
- `POST /api/workorders/{id}/request-restoration` — request grid reconnection
- `POST /api/workorders/{id}/approve-restoration` — chief approves (with rechecks)
- `POST /api/workorders/{id}/interrupt-approval` — interrupt → pending recheck
- `POST /api/workorders/{id}/recheck-complete` — recheck done → pending restoration
- `POST /api/workorders/{id}/fail-parts` — roll back parts, requeue order
- `POST /api/workorders/{id}/escalate` — escalate order
- `POST /api/workorders/{id}/resume` — resume from escalation
- `POST /api/workorders/{id}/close` — close order, release cabin
- `POST /api/workorders/{id}/cancel` — cancel order

### Parts
- `POST /api/parts` — register a spare part
- `GET /api/parts` — list all parts

### System
- `GET /api/notifications` — list system notifications
- `GET /healthz` — health check

## Testing

```bash
go test -timeout=120s -count=1 ./...
```

## Project structure

```
cmd/server/         Entry point and config loading
internal/cabin/     Battery cabin domain (freeze, acquire, recheck)
internal/workorder/ Work order entity and state machine
internal/parts/     Spare-parts inventory and dual-sign requisitions
internal/store/     File-backed JSON persistence
internal/dispatch/  Orchestration: queues, business rules, recovery
internal/scheduler/ Background deadline and timeout monitoring
internal/api/       HTTP handlers and routing
```
