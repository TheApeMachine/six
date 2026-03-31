# six

Quickly putting ideas down.

## Value

The Value type is segmented into regions.

[0...56] Holographic Symbol Data
[57..59] PrevID, NextID, ValueID

## Signals

Instead of actually applying bitwise operations directly on Values, what we do is copy out the data the operations are performed on, and treating the results only as signal. The signal dictates which new Values will be emitted.

RULES:

- If it cancels something is taken out, and some things remain. It means the thing taken out moves "back" and points "forwards". [0000<1111>0000], here <1111> canceled out, so 3 new Values are emitted: 1111 which points to two Values (because the cancellation broke them up), both 0000.
- If it merges, something is added, so it moves forwards. [0000<1111>0000], here let's assume 0000 and 0000 merged, so two new Values are emitted: 1111 which points to the merged 0000.

The examples are likely not very correct, but the idea makes sense, I just don't know bitwise stuff so well.

Very important: the longest sequential operation wins, and becomes the decisive signal.
That means in: `X is in the Y` you will get signal on `is` `is in` and `is in the` which means `is in the` becomes the signal to apply the above rules to.