@0xd8fbcd3b9c8306f2;

using Go = import "/go.capnp";
$Go.package("telemetry");
$Go.import("github.com/theapemachine/six/pkg/telemetry");

struct Event {
  component @0 :Text;
  action    @1 :Text;
  data      @2 :EventData;
}

struct EventData {
  valueId        @0  :Int32;
  bin            @1  :Int32;
  state          @2  :Text;

  activeBits     @3  :List(Int32);
  density        @4  :Float64;
  chunkText      @5  :Text;

  residue        @6  :Int32;
  matchBits      @7  :List(Int32);
  cancelBits     @8  :List(Int32);

  left           @9  :Int32;
  right          @10 :Int32;
  pos            @11 :Int32;

  paths          @12 :Int32;
  chunks         @13 :Int32;
  edges          @14 :Int32;

  level          @15 :Int32;
  theta          @16 :Float64;
  parentBin      @17 :Int32;
  childCount     @18 :Int32;

  stage          @19 :Text;
  message        @20 :Text;
  edgeCount      @21 :Int32;
  pathCount      @22 :Int32;
  resultText     @23 :Text;
  wavefrontEnergy @24 :Int32;
  entryCount     @25 :Int32;

  step           @26 :Int32;
  maxSteps       @27 :Int32;
  candidateCount @28 :Int32;
  bestIndex      @29 :Int32;
  preResidue     @30 :Int32;
  postResidue    @31 :Int32;
  advanced       @32 :Bool;
  stable         @33 :Bool;
  outcome        @34 :Text;
  spanSize       @35 :Int32;
}
