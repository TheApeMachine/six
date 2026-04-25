/*
postexec_layout.h derives post-ALU property words from the generated layout
macros in primitives.h so device post-exec stays aligned with config.yml.
*/
#ifndef SUBSTRATE_POSTEXEC_LAYOUT_H
#define SUBSTRATE_POSTEXEC_LAYOUT_H

#define PROPERTIES_REFUTATION_TARGET_WORD (PROPERTIES_START_WORD + 9)
#define PROPERTIES_TTL_WORD               (PROPERTIES_START_WORD + 3)
#define PROPERTIES_NOISE_WORD             (PROPERTIES_START_WORD + 6)
#define SCHEDULING_NEXT_PROGRAM_WORD      (PROPERTIES_START_WORD + 15)
#define SIGNALS_FALSIFIED_WORD            (SIGNALS_START_WORD + 7)

#define TTL_EXPIRED_SENTINEL              (1ULL << 63)
#define FALSIFIED_SIGNAL_SENTINEL         1ULL

#define REFUTATION_ONE_RUN_THRESHOLD      48

#endif
