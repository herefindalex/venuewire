# VenueWire project identity

VenueWire is the canonical project name for this multi-venue Testnet connector.

The canonical identifiers are:

- Go module: `venuewire`
- CLI source: `./cmd/venuewire`
- CLI executable: `./bin/venuewire`
- E2E executable override: `VENUEWIRE_BINARY`
- default Deribit FIX sender component ID: `venuewire`

Exchange-specific identifiers remain unchanged. In particular, `BYBIT_*`,
`DERIBIT_*`, `RUN_BYBIT_*`, `RUN_DERIBIT_*`, and `--venue bybit` describe
exchange behavior rather than the project name.

For compatibility with existing automation, the E2E runner still accepts
`BYBITCTL_BINARY` when `VENUEWIRE_BINARY` is unset and emits a deprecation
warning. New automation must use `VENUEWIRE_BINARY`.
