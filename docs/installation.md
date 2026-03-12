# Installation

## Requirements

- Go 1.22+
- At least one backend:
  - Valkey or Redis 7+ (for ValkeyStore)
  - PostgreSQL 12+ (for PostgresStore)

## Install

```sh
go get codeberg.org/matthew/capacitor
```

## Backend Dependencies

### Valkey (optional)

No additional import needed. Uses `github.com/valkey-io/valkey-go` which is included.

### PostgreSQL (optional)

If using PostgreSQL, add the pgx driver:

```sh
go get github.com/jackc/pgx/v5
```

## Verify Installation

```go
package main

import (
    "fmt"
    "github.com/valkey-io/valkey-go"
    "codeberg.org/matthew/capacitor"
)

func main() {
    client, _ := valkey.NewClient(valkey.ClientOption{
        InitAddress: []string{"localhost:6379"},
    })
    
    store := capacitor.NewValkeyStore(client, capacitor.DefaultConfig())
    limiter := capacitor.New(store, capacitor.DefaultConfig())
    
    fmt.Println("Capacitor installed successfully!")
    _ = limiter
}
```
