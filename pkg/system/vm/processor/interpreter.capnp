@0xad054c9d70313d87;

using Go = import "/go.capnp";
$Go.package("processor");
$Go.import("github.com/theapemachine/six/pkg/system/vm/processor");

using Primitive = import "../../../logic/lang/primitive/value.capnp";

struct Loader {
  values @0 :List(Primitive.Value);
  buffer @1 :Data;
}

interface Interpreter extends(Primitive.Service) {
  # Inherits read/write/close from Primitive.Service.
  # Values written carry their own dispatch registers:
  #   C5  Opcode      — control flow (Next, Jump, Branch, Halt)
  #   C6              — residual / carry channel (operand extension)
  #   C7  shell word  — RouteHint, Trajectory (from/to phases), affine op, guard radius, flags
  #   C0–C3 + C4:0    — 257-bit GF(257) core operand state
}
