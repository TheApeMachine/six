@0xad058c9d70413d69;

using Go = import "/go.capnp";
$Go.package("semantic");
$Go.import("github.com/theapemachine/six/pkg/logic/semantic");

interface Engine {
  prompt @0 (msg :Text) -> (result :Text);
  inject @1 (subject :Text, link :Text, object :Text) -> (braid :UInt32);
  query  @2 (braid :UInt32, knownA :Text, knownB :Text, axis :UInt8) -> (name :Text, phase :UInt32);
}


