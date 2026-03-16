@0xad058c9d70413d6b;

using Go = import "/go.capnp";
$Go.package("goal");
$Go.import("github.com/theapemachine/six/pkg/logic/synthesis/goal");

interface Frustration {
  prompt @0 (msg :Text) -> (result :Text);
}


