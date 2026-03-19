@0xad058c9d70413d3c;

using Go = import "/go.capnp";
$Go.package("synthesis");
$Go.import("github.com/theapemachine/six/pkg/logic/synthesis");

using import "../../store/data/value.capnp".Value;

interface HAS {
  write @0 (start :Value, end :Value) -> stream;
  done  @1 () -> (
    keyText     :Text,
    useCount    :UInt64,
    hardened    :Bool,
    winnerIndex :Int32,
    postResidue :Int32,
    steps       :UInt32
  );
}
