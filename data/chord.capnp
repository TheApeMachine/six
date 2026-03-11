@0xad058c9d70413d68;

using Go = import "/go.capnp";
$Go.package("data");
$Go.import("github.com/theapemachine/six/data");

struct Chord {
  c0 @0 :UInt64;
  c1 @1 :UInt64;
  c2 @2 :UInt64;
  c3 @3 :UInt64;
  c4 @4 :UInt64;
  c5 @5 :UInt64;
  c6 @6 :UInt64;
  c7 @7 :UInt64;
}
