# FIX fixtures

Fixtures use `|` as a visible representation of SOH (`0x01`). Tests replace
the delimiter before parsing, so byte-level BodyLength and CheckSum validation
still runs against the actual wire representation.
