package primitive

import (
	"testing"

	. "github.com/smartystreets/goconvey/convey"
	"github.com/theapemachine/six/pkg/core"
)

func TestThermodynamicDecayEvaporation(t *testing.T) {

	Convey("Energy decays to zero and exhaustion is detected", t, func() {
		savedWord := core.Cfg.System.ThermodynamicEnergyWord
		savedBirth := core.Cfg.System.ThermodynamicBirthEnergy
		savedDecay := core.Cfg.System.ThermodynamicDecayDelta

		defer func() {
			core.Cfg.System.ThermodynamicEnergyWord = savedWord
			core.Cfg.System.ThermodynamicBirthEnergy = savedBirth
			core.Cfg.System.ThermodynamicDecayDelta = savedDecay
		}()

		core.Cfg.System.ThermodynamicEnergyWord = core.Cfg.Value.Region.Registers.R9
		core.Cfg.System.ThermodynamicBirthEnergy = 3
		core.Cfg.System.ThermodynamicDecayDelta = 1

		var frame [128]uint64
		SeedThermodynamicEnergy(&frame)
		So(frame[core.Cfg.Value.Region.Registers.R9]&0xFFFF, ShouldEqual, 3)

		ApplyThermodynamicDecay(&frame)
		So(frame[core.Cfg.Value.Region.Registers.R9]&0xFFFF, ShouldEqual, 2)

		ApplyThermodynamicDecay(&frame)
		ApplyThermodynamicDecay(&frame)
		So(ThermodynamicIsExhausted(&frame), ShouldBeTrue)
	})
}
