# Snapshot v1 to v2 migration

Snapshot v2 adds explicit venue, Testnet environment, and stable account alias identity. It is intentionally not migrated on application startup.

Preview without writing:

```bash
./bin/venuewire state migrate-v1 --bybit-account-alias bybit-test --dry-run
```

Apply after reviewing the report:

```bash
./bin/venuewire state migrate-v1 --bybit-account-alias bybit-test
```

The apply operation creates `state/orders.json.v1.bak` with mode `0600` before replacing the snapshot. An existing backup is never overwritten. A record with no reliable category or stable identifier aborts the entire migration.

Rollback:

```bash
./bin/venuewire state restore-v1
```

Rollback validates the backup as v1 and atomically restores its exact bytes. The backup is retained. A non-default Bybit account alias uses compound storage keys; `bybit-test` retains legacy map keys for compatibility while the values carry full account identity.
