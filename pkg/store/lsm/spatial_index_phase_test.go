package lsm

import (
	"testing"

	"capnproto.org/go/capnp/v3"
	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/numeric"
	"github.com/theapemachine/six/pkg/store/data"
)

func buildPhaseIndex(input []byte) *SpatialIndexServer {
	idx := NewSpatialIndexServer()
	calc := numeric.NewCalculus()
	state := numeric.Phase(1)

	for pos, symbol := range input {
		state = calc.Multiply(state, calc.Power(3, uint32(symbol)))

		value := data.BaseValue(symbol)
		value.Set(int(state))
		value.SetResidualCarry(uint64(state))
		value.SetProgram(data.OpcodeNext, 1, 0, false)
		if pos == len(input)-1 {
			value.SetProgram(data.OpcodeHalt, 0, 0, true)
		}

		idx.insertSync(morton.Pack(uint32(pos), symbol), value, data.MustNewValue())
	}

	return idx
}

func buildPromptValueList(input []byte) (data.Value_List, error) {
	_, seg, err := capnp.NewMessage(capnp.SingleSegment(nil))
	if err != nil {
		return data.Value_List{}, err
	}

	list, err := data.NewValue_List(seg, int32(len(input)))
	if err != nil {
		return data.Value_List{}, err
	}

	calc := numeric.NewCalculus()
	state := numeric.Phase(1)

	for pos, symbol := range input {
		state = calc.Multiply(state, calc.Power(3, uint32(symbol)))

		value := data.BaseValue(symbol)
		value.Set(int(state))
		value.SetResidualCarry(uint64(state))
		value.SetProgram(data.OpcodeNext, 1, 0, false)

		dst := list.At(pos)
		dst.CopyFrom(value)
	}

	return list, nil
}

func TestSpatialIndexBuildPathsUsesPhaseTraversal(t *testing.T) {
	Convey("Given a byte-addressed spatial index", t, func() {
		idx := buildPhaseIndex([]byte("Roy is in the Kitchen"))
		promptList, err := buildPromptValueList([]byte("Roy is in the "))
		So(err, ShouldBeNil)

		Convey("buildPaths should recover the continuation via prompt phase", func() {
			paths, metaPaths, err := idx.buildPaths(promptList)
			So(err, ShouldBeNil)
			So(len(paths), ShouldBeGreaterThan, 0)
			So(len(metaPaths), ShouldEqual, len(paths))

			decoded := idx.decodeValues(paths[0])
			So(len(decoded), ShouldBeGreaterThan, 0)

			var joined string
			for _, seq := range decoded {
				joined += string(seq)
			}

			So(joined, ShouldContainSubstring, "Kitchen")
		})

		Convey("buildPaths should fall back to prompt wavefront when the prompt has a substitution typo", func() {
			promptList, err := buildPromptValueList([]byte("Roy is in the Kitchan"))
			So(err, ShouldBeNil)

			paths, metaPaths, err := idx.buildPaths(promptList)
			So(err, ShouldBeNil)
			So(len(paths), ShouldBeGreaterThan, 0)
			So(len(metaPaths), ShouldEqual, len(paths))

			decoded := idx.decodeValues(paths[0])
			So(len(decoded), ShouldBeGreaterThan, 0)
			So(string(decoded[0]), ShouldContainSubstring, "Kitchen")
		})

		Convey("buildPaths should survive a deleted prompt byte", func() {
			promptList, err := buildPromptValueList([]byte("Roy is in the Kithen"))
			So(err, ShouldBeNil)

			paths, metaPaths, err := idx.buildPaths(promptList)
			So(err, ShouldBeNil)
			So(len(paths), ShouldBeGreaterThan, 0)
			So(len(metaPaths), ShouldEqual, len(paths))

			decoded := idx.decodeValues(paths[0])
			So(len(decoded), ShouldBeGreaterThan, 0)
			So(string(decoded[0]), ShouldContainSubstring, "Kitchen")
		})

		Convey("buildPaths should survive an inserted prompt byte", func() {
			promptList, err := buildPromptValueList([]byte("Roy is in the Kittchen"))
			So(err, ShouldBeNil)

			paths, metaPaths, err := idx.buildPaths(promptList)
			So(err, ShouldBeNil)
			So(len(paths), ShouldBeGreaterThan, 0)
			So(len(metaPaths), ShouldEqual, len(paths))

			decoded := idx.decodeValues(paths[0])
			So(len(decoded), ShouldBeGreaterThan, 0)
			So(string(decoded[0]), ShouldContainSubstring, "Kitchen")
		})
	})
}

func TestSpatialIndexStoresNativeValuesButReturnsObservables(t *testing.T) {
	Convey("Given an observable tokenizer value inserted into the spatial index", t, func() {
		idx := NewSpatialIndexServer()
		symbol := byte('A')
		phase := numeric.Phase(17)

		observable := data.BaseValue(symbol)
		observable.Set(int(phase))
		observable.SetResidualCarry(uint64(phase))
		observable.SetProgram(data.OpcodeHalt, 0, 0, true)

		key := morton.Pack(0, symbol)
		idx.insertSync(key, observable, data.MustNewValue())

		stored := idx.GetEntry(key)
		So(data.HasLexicalSeed(stored, symbol), ShouldBeFalse)
		So(stored.ResidualCarry(), ShouldEqual, uint64(phase))

		projected := data.ObservableValue(symbol, stored)
		decoded, ok := inferByteFromValue(projected)
		So(ok, ShouldBeTrue)
		So(decoded, ShouldEqual, symbol)
	})
}
