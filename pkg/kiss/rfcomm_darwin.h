//go:build darwin && cgo

#ifndef GRAYWOLF_RFCOMM_DARWIN_H
#define GRAYWOLF_RFCOMM_DARWIN_H

#include <stdint.h>

// All functions are thread-safe. Error text is written into err (NUL
// terminated, truncated to errlen).

// Opens an RFCOMM (SPP) channel to the paired Classic Bluetooth device at
// addr. channelHint 0 means "look up the Serial Port service via SDP".
// On success returns a non-zero session handle and a pipe read fd in
// *rfd on which received bytes arrive; EOF on the pipe means the link
// closed.
uintptr_t gw_rfcomm_open(const char *addr, int channelHint, int *rfd,
                         char *err, int errlen);

// Writes n bytes, blocking up to timeoutMs. Returns 0 on success.
int gw_rfcomm_write(uintptr_t h, const void *buf, int n, int timeoutMs,
                    char *err, int errlen);

// Closes the channel and the baseband connection and ends the pipe.
void gw_rfcomm_close(uintptr_t h);

// Returns a malloc'd JSON array [{"mac":"AA:BB:..","name":".."}] of paired
// devices; the caller frees it. NULL on failure.
char *gw_bt_paired_json(void);

#endif
