package compute

import (
	"context"
	"fmt"

	"github.com/theapemachine/six/pkg/core"
	"github.com/theapemachine/six/pkg/primitive"
)

/*
Firmware owns the lifecycle context for emitted firmware Values and carries
the compiled program table loaded from config. Deploy uses that table to
validate requested firmware before installing the packed words into Values.
*/
type Firmware struct {
	ctx      context.Context
	cancel   context.CancelFunc
	err      error
	Programs map[core.FirmwareType]core.ProgramConfig
}

/*
NewFirmware creates a new Firmware instance.
*/
func NewFirmware(ctx context.Context) *Firmware {
	ctx, cancel := context.WithCancel(ctx)

	programs := make(map[core.FirmwareType]core.ProgramConfig)
	if core.Cfg != nil {
		for name, program := range core.Cfg.Programs {
			programs[name] = program
		}
	}

	return &Firmware{
		ctx:      ctx,
		cancel:   cancel,
		Programs: programs,
	}
}

/*
Deploy a new programmed Value with the requested firmware
*/
func (firmware *Firmware) Deploy(
	name core.FirmwareType,
	queryInputs []uint64,
	inputs []uint64,
) ([]*primitive.Value, error) {
	if firmware == nil {
		return nil, fmt.Errorf("compute.Firmware.Deploy: firmware is nil")
	}

	if _, ok := firmware.Programs[name]; !ok {
		return nil, fmt.Errorf("compute.Firmware.Deploy: firmware %q is not compiled", name)
	}

	if _, ok := firmware.Programs[core.QUERY]; !ok {
		return nil, fmt.Errorf("compute.Firmware.Deploy: firmware %q is not compiled", core.QUERY)
	}

	value := primitive.Emit()
	value.SetProperty(primitive.REFERENCE, value.ID())

	for idx, input := range inputs {
		value.Set(core.Cfg.Value.Region.Context.Start+idx, input)
	}

	if _, err := value.InstallFirmware(name); err != nil {
		value.Close()
		return nil, fmt.Errorf("compute.Firmware.Deploy: install firmware %q: %w", name, err)
	}

	value.SetStatus(primitive.WAITING)

	query := primitive.Emit(
		primitive.WithReference(value.ID()),
		primitive.WithContinuation(value.ID()),
	)

	for idx, input := range queryInputs {
		query.Set(core.Cfg.Value.Region.Context.Start+idx, input)
	}

	if _, err := query.InstallFirmware(core.QUERY); err != nil {
		primitive.CloseAll([]*primitive.Value{value, query})
		return nil, fmt.Errorf("compute.Firmware.Deploy: install firmware %q: %w", core.QUERY, err)
	}

	query.SetStatus(primitive.READY)

	return []*primitive.Value{value, query}, nil
}

/*
Close the Firmware instance.
*/
func (firmware *Firmware) Close() error {
	if firmware == nil {
		return nil
	}

	firmware.cancel()

	return firmware.err
}

/*
Error returns the error of the Firmware instance.
*/
func (firmware *Firmware) Error() error {
	return firmware.err
}
