@0xad058c9d70413d67;

using Go = import "/go.capnp";
$Go.package("graph");
$Go.import("github.com/theapemachine/six/logic/graph");

using import "../../data/chord.capnp".Chord;

struct GraphEdge {
  left     @0 :UInt8;
  right    @1 :UInt8;
  position @2 :UInt32;
  chord    @3 :Chord;
}

interface Matrix {
  # Streaming ingestion for 0-copy, zero-blocking insertions
  prompt           @0 (chords :List(Chord)) -> (paths :List(List(Chord)));
  done             @1 ();
}
