package compute

import (
	"context"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Firmware holds the compiled programs defined in the config,
and provides a convenient way to emit a programmed Value.
*/
type Firmware struct {
	ctx    context.Context
	cancel context.CancelFunc
	err    error
}

/*
NewFirmware creates a new Firmware instance.
*/
func NewFirmware(ctx context.Context) *Firmware {
	ctx, cancel := context.WithCancel(ctx)

	return &Firmware{
		ctx:    ctx,
		cancel: cancel,
	}
}

/*
Deploy a new programmed Value with the requested firmware
*/
func (firmware *Firmware) Deploy(
	name core.FirmwareType,
	q []uint64,
	inputs []uint64,
) []*primitive.Value {
	value := primitive.Emit()

	for idx, input := range inputs {
		value.Set(core.Cfg.Value.Region.Context.Start+idx, input)
	}

	if _, err := value.InstallFirmware(name); err != nil {
		panic(err)
	}

	value.SetStatus(primitive.WAITING)

	query := primitive.Emit(
		primitive.WithReference(value.ID()),
		primitive.WithContinuation(value.ID()),
	)

	for idx, input := range q {
		query.Set(core.Cfg.Value.Region.Context.Start+idx, input)
	}

	if _, err := query.InstallFirmware(core.QUERY); err != nil {
		panic(err)
	}

	query.SetStatus(primitive.READY)

	return []*primitive.Value{value, query}
}

/*
Close the Firmware instance.
*/
func (firmware *Firmware) Close() (err error) {
	firmware.cancel()
	return err
}

/*
Error returns the error of the Firmware instance.
*/
func (firmware *Firmware) Error() error {
	return firmware.err
}
