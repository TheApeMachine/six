@0xad058c9d70413d6c;

using Go = import "/go.capnp";
$Go.package("macro");
$Go.import("github.com/theapemachine/six/pkg/logic/synthesis/macro");

using import "../../../logic/lang/primitive/value.capnp".Value;

interface MacroIndex {
  write @0 (start :Value, end :Value) -> stream;
  done  @1 () -> (keyText :Text, useCount :UInt64, hardened :Bool);
  resolveGap @2 (start :Value, end :Value) -> (
    scale     :UInt32,
    translate :UInt32,
    useCount  :UInt64,
    hardened  :Bool
  );
}
