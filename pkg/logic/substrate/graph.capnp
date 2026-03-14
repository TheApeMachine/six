@0xad058c9d70413d67;

using Go = import "/go.capnp";
$Go.package("substrate");
$Go.import("github.com/theapemachine/six/pkg/logic/substrate");

using import "../../store/data/chord.capnp".Chord;

struct GraphEdge {
  left     @0 :UInt32;
  right    @1 :UInt32;
  position @2 :UInt32;
  chord    @3 :Chord;
}

interface Graph {
  # Machine delivers pre-fetched paths; Graph reasons over them.
  prompt @0 (paths :List(List(Chord)), metaPaths :List(List(Chord))) -> (result :List(List(Chord)));
  done   @1 ();
}
