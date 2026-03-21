@0xad058c9d70413d67;

using Go = import "/go.capnp";
$Go.package("substrate");
$Go.import("github.com/theapemachine/six/pkg/logic/substrate");

using import "../lang/primitive/value.capnp".Value;

struct GraphEdge {
  left     @0 :UInt32;
  right    @1 :UInt32;
  position @2 :UInt32;
  value    @3 :Value;
}

interface Graph {
  # Machine delivers pre-fetched paths; Graph reasons over them.
  write      @0 (key :UInt64) -> stream;
  done       @1 ();
  writeBatch @2 (keys :List(UInt64)) -> stream;
}
