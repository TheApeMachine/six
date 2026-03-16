@0xad058c9d70413d66;

using Go = import "/go.capnp";
$Go.package("lsm");
$Go.import("github.com/theapemachine/six/pkg/store/lsm");

using import "../data/value.capnp".Value;

struct GraphEdge {
  left     @0 :UInt8;
  right    @1 :UInt8;
  position @2 :UInt32;
  value    @3 :Value;
  meta     @4 :Value;
}

interface SpatialIndex {
  # Streaming ingestion for 0-copy, zero-blocking insertions
  insert           @0 (edge :GraphEdge) -> stream;
  done             @1 ();

  # Fast-path lookups
  lookup           @2 (values :List(Value)) -> (paths :List(List(Value)), metaPaths :List(List(Value)));
  queryTransitions @3 (left :UInt8, position :UInt32) -> (values :List(Value), metas :List(Value));

  # Stateful path decode: result values back into byte sequences
  decode           @4 (values :List(List(Value))) -> (sequences :List(Data));
}


