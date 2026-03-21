package tokenizer

import "context"

/*
DrainKeys finalizes a tokenizer stream using the shared two-pass done-drain.
Some streams emit their last batch only after the first Done call, so callers
must drain twice to collect the complete key sequence.
*/
func DrainKeys(ctx context.Context, client Universal) ([]uint64, error) {
	keys := make([]uint64, 0)

	for range 2 {
		future, release := client.Done(ctx, nil)

		results, err := future.Struct()
		if err != nil {
			release()
			return nil, err
		}

		list, err := results.Keys()
		if err != nil {
			release()
			return nil, err
		}

		for index := range list.Len() {
			keys = append(keys, list.At(index))
		}

		release()
	}

	return keys, nil
}
