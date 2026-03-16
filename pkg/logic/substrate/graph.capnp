@0xad058c9d70413d67;

using Go = import "/go.capnp";
$Go.package("substrate");
$Go.import("github.com/theapemachine/six/pkg/logic/substrate");

using import "../../store/data/value.capnp".Value;

struct GraphEdge {
  left     @0 :UInt32;
  right    @1 :UInt32;
  position @2 :UInt32;
  value    @3 :Value;
}

interface Graph {
  # Machine delivers pre-fetched paths; Graph reasons over them.
  prompt @0 (paths :List(List(Value)), metaPaths :List(List(Value))) -> (result :List(List(Value)));
  done   @1 ();
}


