@0xad058c9d70413d66;

using Go = import "/go.capnp";
$Go.package("lsm");
$Go.import("github.com/theapemachine/six/store/lsm");

struct Chord {
  c0 @0 :UInt64;
  c1 @1 :UInt64;
  c2 @2 :UInt64;
  c3 @3 :UInt64;
  c4 @4 :UInt64;
  c5 @5 :UInt64;
  c6 @6 :UInt64;
  c7 @7 :UInt64;
}

struct GraphEdge {
  left     @0 :UInt8;
  right    @1 :UInt8;
  position @2 :UInt32;
  chord    @3 :Chord;
}

interface SpatialIndex {
  # Streaming ingestion for 0-copy, zero-blocking insertions
  insert           @0 (edge :GraphEdge) -> stream;
  done             @1 ();

  # Fast-path lookups
  lookup           @2 (left :UInt8, right :UInt8, position :UInt32) -> (chord :Chord);
  queryTransitions @3 (left :UInt8, position :UInt32) -> (chords :List(Chord));
}
