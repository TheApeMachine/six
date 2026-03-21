@0xad058c9d70413d66;

using Go = import "/go.capnp";
$Go.package("server");
$Go.import("github.com/theapemachine/six/pkg/store/dmt/server");

using import "../../../logic/lang/primitive/value.capnp".Value;

interface Server {
  write @0 (key :UInt64) -> stream;
  done  @1 () -> (keys :List(UInt64));
  branches @2 (prompt :Value) -> (branches :List(Value));
  writeBatch @3 (keys :List(UInt64)) -> stream;
}
