# Backends

Capacitor supports multiple backend stores through a common `Store` interface. Choose the backend that fits your infrastructure.

## Available Backends

| Backend | Package | Use When |
|---------|---------|----------|
| Valkey | `capacitor` (default) | Low latency, existing Valkey/Redis |
| PostgreSQL | `capacitor` | Existing PostgreSQL, no extra services |
| Mock | `capacitor_test` | Testing |

See [Valkey](valkey.md) and [PostgreSQL](postgres.md) for detailed comparisons.
