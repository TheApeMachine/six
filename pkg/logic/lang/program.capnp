@0xad058c9d70413d87;

using Go = import "/go.capnp";
$Go.package("lang");
$Go.import("github.com/theapemachine/six/pkg/logic/lang");

using Primitive = import "./primitive/value.capnp";

struct Program {
  values @0 :List(Primitive.Value);
  buffer @1 :Data;
}

interface Service extends(Primitive.Service) {
  # Inherits from Primitive.Service
}