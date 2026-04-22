#ifndef SIX_DEVICE_POSTEXEC_METAL
#define SIX_DEVICE_POSTEXEC_METAL

static inline int longest_one_run_signals(device const ulong* frame) {
    int bestLen = 0;
    int curLen = 0;

    for (int wi = 0; wi < 8; wi++) {
        ulong word = frame[SIGNALS_START_WORD + wi];
        if (word == ~0UL) {
            curLen += 64;
            continue;
        }
        if (word == 0UL) {
            if (curLen > bestLen) {
                bestLen = curLen;
            }
            curLen = 0;
            continue;
        }
        for (int bit = 0; bit < 64; bit++) {
            if ((word >> bit) & 1UL) {
                curLen++;
            } else {
                if (curLen > bestLen) {
                    bestLen = curLen;
                }
                curLen = 0;
            }
        }
    }
    if (curLen > bestLen) {
        bestLen = curLen;
    }
    return bestLen;
}

static inline void apply_post_execution_lifecycle(device ulong* frame) {
    ulong word = frame[PROPERTIES_TTL_WORD];
    if (word == 0UL || word == ~0UL) {
        return;
    }
    if (word & TTL_EXPIRED_SENTINEL) {
        return;
    }
    if (word == 1UL) {
        frame[PROPERTIES_TTL_WORD] = TTL_EXPIRED_SENTINEL;
        frame[SCHEDULING_NEXT_PROGRAM_WORD] = 0UL;
        return;
    }
    frame[PROPERTIES_TTL_WORD] = word - 1UL;
}

static inline void apply_refutation_probe(device ulong* frame) {
    ulong target = frame[PROPERTIES_REFUTATION_TARGET_WORD];
    if (target == 0UL) {
        return;
    }
    if (longest_one_run_signals(frame) < REFUTATION_ONE_RUN_THRESHOLD) {
        return;
    }
    frame[PROPERTIES_NOISE_WORD] |= FALSIFIED_BIT_NOISE_WORD;
    frame[SCHEDULING_NEXT_PROGRAM_WORD] = 0UL;
    frame[PROPERTIES_REFUTATION_TARGET_WORD] = 0UL;
}

static inline void finish_frame_post_alu_device(device ulong* frame) {
    apply_refutation_probe(frame);
    apply_post_execution_lifecycle(frame);
}

#endif


