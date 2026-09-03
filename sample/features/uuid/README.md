# uuid

RFC 9562 UUIDs as canonical 36-character lowercase strings — mhl has no dedicated UUID
type, so `uuid.*` produces the same `string` shape everything else consumes. All non-fixed
bits come from `crypto/rand` (stdlib, no new dependency); an entropy failure raises.

- [uuid_v4_is_canonical_random.mh](uuid_v4_is_canonical_random.mh) — `uuid.v4()` returns a
  random version-4 UUID; a fresh one every call
- [uuid_v7_carries_a_timestamp_prefix.mh](uuid_v7_carries_a_timestamp_prefix.mh) —
  `uuid.v7()` prefixes a 48-bit millisecond timestamp, so values minted together sort in
  creation order while the random tail keeps them distinct
