/*
postexec_layout.h mirrors pkg/compute/kernel/layout.go word indices and
post-ALU constants for device kernels. Keep in sync when layout changes.
*/
#ifndef SUBSTRATE_POSTEXEC_LAYOUT_H
#define SUBSTRATE_POSTEXEC_LAYOUT_H

#define PROPERTIES_REFUTATION_TARGET_WORD 49
#define PROPERTIES_TTL_WORD               51
#define PROPERTIES_NOISE_WORD             52
#define SCHEDULING_NEXT_PROGRAM_WORD      117

#define TTL_EXPIRED_SENTINEL              (1ULL << 63)
#define FALSIFIED_BIT_NOISE_WORD          (1ULL << 62)

#define REFUTATION_ONE_RUN_THRESHOLD      48

#define OPCODE_EMIT_CLONE                 0x60

#define SPAWN_QUEUE_CAP                   4096

#endif
