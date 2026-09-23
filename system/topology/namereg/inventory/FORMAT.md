# Naming participant inventory v1

This is a retained KV record format, independent of the mesh frame protocol
and of any future runtime implementation. It records candidate participation;
reading or writing it does not grant name-admission authority. A future
activation protocol must establish authoritative snapshot and fencing rules.

The default key is `_sys:naming:participants:v1`. Integers use unsigned,
big-endian encoding. Variable-width fields contain raw bytes; domain and node
identifiers are nonempty, length-bounded byte strings without Unicode
normalization. Consumers compare node IDs by unsigned byte order. The value
has exactly this layout, with no trailing bytes:

| Field | Encoding |
| --- | --- |
| Magic | Four bytes `NINV` |
| Record format | One byte, `1` |
| Domain | `uint16` byte length, then bytes |
| Generation | `uint64`, initially `1`, advanced on membership transitions |
| Slots | `uint16` count, then the following fields for each slot |
| Node | `uint16` byte length, then bytes; strictly increasing across slots |
| Incarnation sequence | `uint64`, nonzero |
| Boot nonce | `uint16` byte length, then nonempty bytes |
| Retired | One byte: `0` or `1` |
| Retired sequence high-water | `uint64`; retained when a slot retires |

All slots, including tombstones, remain in the record. A node's first
incarnation uses sequence `1` and high-water `0`. A live slot always has
high-water equal to sequence minus one; a retired slot has high-water equal to
its retired sequence. A live incarnation must retire before another incarnation
of its node may enroll; the successor uses exactly high-water plus one with a
caller-supplied boot nonce. Generation and sequence overflow fail closed.
Initialization requires an absent key; later transitions require
the caller's exact observed KV revision as a transaction precondition.

The default limits are 4,096 slots, 128-byte domain and node IDs, a 64-byte
nonce and a 1 MiB record. Decoders reject unknown versions, noncanonical slot
order, invalid states, impossible lengths and trailing bytes. The golden byte
fixture in `TestInventoryWireFixture` anchors the encoding for independent
implementations. This record must not be mistaken for a versioned mesh
message or an authoritative, linearizable readiness snapshot.
