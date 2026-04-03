package primitive

import (
	"github.com/theapemachine/six/pkg/core"
)

/*
thermodynamicEnergyWord resolves the configured word index for energy storage.
Low 16 bits carry remaining energy; high bits are left zero by this package.
*/
func thermodynamicEnergyWord() int {

	if core.Cfg.System.ThermodynamicEnergyWord >= 0 {
		return core.Cfg.System.ThermodynamicEnergyWord
	}

	return core.Cfg.Value.Region.Registers.R9
}

/*
SeedThermodynamicEnergy sets birth energy on a new or emitted frame.
*/
func SeedThermodynamicEnergy(frame *[128]uint64) {

	if frame == nil {
		return
	}

	n := core.Cfg.System.ThermodynamicBirthEnergy
	if n <= 0 {
		n = 256
	}

	if n > 0xFFFF {
		n = 0xFFFF
	}

	w := thermodynamicEnergyWord()
	if w < 0 || w >= len(frame) {
		return
	}

	frame[w] = (frame[w] &^ 0xFFFF) | uint64(n)
}

/*
ApplyThermodynamicDecay subtracts decay from energy for frames touched this batch.
*/
func ApplyThermodynamicDecay(frame *[128]uint64) {

	if frame == nil {
		return
	}

	delta := core.Cfg.System.ThermodynamicDecayDelta
	if delta <= 0 {
		delta = 1
	}

	w := thermodynamicEnergyWord()
	if w < 0 || w >= len(frame) {
		return
	}

	e := int(frame[w] & 0xFFFF)
	e -= delta
	if e < 0 {
		e = 0
	}

	frame[w] = (frame[w] &^ 0xFFFF) | uint64(e)
}

/*
ThermodynamicGain adds energy (capped) after a reinforcing substrate event.
*/
func ThermodynamicGain(frame *[128]uint64, amount int) {

	if frame == nil || amount <= 0 {
		return
	}

	w := thermodynamicEnergyWord()
	if w < 0 || w >= len(frame) {
		return
	}

	e := int(frame[w] & 0xFFFF)
	e += amount
	if e > 0xFFFF {
		e = 0xFFFF
	}

	frame[w] = (frame[w] &^ 0xFFFF) | uint64(e)
}

/*
ThermodynamicIsExhausted is true when energy word is zero.
*/
func ThermodynamicIsExhausted(frame *[128]uint64) bool {

	if frame == nil {
		return false
	}

	w := thermodynamicEnergyWord()
	if w < 0 || w >= len(frame) {
		return false
	}

	return frame[w]&0xFFFF == 0
}
