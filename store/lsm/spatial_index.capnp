@0xad058c9d70413d66;

using Go = import "/go.capnp";
$Go.package("lsm");
$Go.import("github.com/theapemachine/six/store/lsm");

using import "../../data/chord.capnp".Chord;

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
  lookup           @2 (chords :List(Chord)) -> (paths :List(List(Chord)));
  queryTransitions @3 (left :UInt8, position :UInt32) -> (chords :List(Chord));
}
