/*
postexec_layout.h derives post-ALU property words from the generated layout
macros in primitives.h so device post-exec stays aligned with config.yml.
*/
#ifndef SUBSTRATE_POSTEXEC_LAYOUT_H
#define SUBSTRATE_POSTEXEC_LAYOUT_H

#define PROPERTIES_REFUTATION_TARGET_WORD (PROPERTIES_START_WORD + 1)
#define PROPERTIES_TTL_WORD               (PROPERTIES_START_WORD + 3)
#define PROPERTIES_NOISE_WORD             (PROPERTIES_START_WORD + 4)
#define SCHEDULING_NEXT_PROGRAM_WORD      117

#define TTL_EXPIRED_SENTINEL              (1ULL << 63)
#define FALSIFIED_BIT_NOISE_WORD          (1ULL << 62)

#define REFUTATION_ONE_RUN_THRESHOLD      48

#endif
