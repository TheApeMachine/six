@0xad058c9d70413d98;

using Go = import "/go.capnp";
$Go.package("primitive");
$Go.import("github.com/theapemachine/six/pkg/logic/lang/primitive");

struct Value {
  blocks @0 :Data;
}

interface Service {
  read  @0 (callback :Callback) -> ();
  write @1 (value :Value) -> stream;
  close @2 ();

  interface Callback {
    send @0 (value :Value) -> stream;
    done @1 ();
  }
}
