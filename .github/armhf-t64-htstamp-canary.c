/*
 * Regression canary for the 32-bit ARM t64 ALSA crash (issue #231).
 *
 * On a t64 system, libasound's `struct timespec` is 16 bytes even on 32-bit
 * ARM, where Rust's default libc::timespec is 8 bytes. `snd_pcm_status_get_*
 * htstamp()` copies that 16-byte struct into the caller's pointer, so an
 * 8-byte buffer (what stock alsa-rs used) gets overrun by 8 bytes -> stack
 * smash -> SIGSEGV. graywolf's fix hands the call a 16-byte buffer.
 *
 * This measures the bytes the C library actually writes by placing a guard
 * value just past buffers of size 8 and 16 and checking whether it survives.
 * Built and run under QEMU in a 32-bit-ARM t64 userland by the
 * armhf-t64-htstamp.yml workflow. No sound hardware needed -- a malloc'd
 * status struct is enough.
 *
 * Expected on a real t64 libasound:
 *   - 8-byte buffer  -> guard clobbered (the bug is real here)
 *   - 16-byte buffer -> guard intact    (the fix's buffer size is sufficient)
 */
#include <alsa/asoundlib.h>
#include <stdio.h>
#include <string.h>

#define GUARD 0xA5A5A5A5DEADBEEFull

/* Returns 1 if the C call wrote past `bufsize` bytes, 0 otherwise. */
static int guard_clobbered(size_t bufsize) {
    snd_pcm_status_t *st = NULL;
    if (snd_pcm_status_malloc(&st) < 0) {
        fprintf(stderr, "snd_pcm_status_malloc failed\n");
        return -1;
    }
    /* bufsize bytes of scratch, then an 8-byte guard, in one aligned block. */
    unsigned char block[32];
    memset(block, 0, sizeof block);
    unsigned long long *guard =
        (unsigned long long *)(block + bufsize);
    *guard = GUARD;

    snd_pcm_status_get_htstamp(st, (snd_htimestamp_t *)block);

    int clobbered = (*guard != GUARD);
    snd_pcm_status_free(st);
    return clobbered;
}

int main(void) {
    int c8 = guard_clobbered(8);
    int c16 = guard_clobbered(16);
    printf("htstamp write test: 8-byte guard clobbered=%d, 16-byte guard clobbered=%d\n",
           c8, c16);

    if (c8 < 0 || c16 < 0)
        return 3;

    /* Fail closed if the 8-byte buffer was NOT overrun: this image is not a
       t64 32-bit userland, so the test is not actually exercising the bug. */
    if (!c8) {
        fprintf(stderr,
            "FAIL: 8-byte buffer not overrun -- not a t64 libasound? "
            "this job must run a 32-bit t64 userland to be meaningful\n");
        return 2;
    }
    /* The fix relies on 16 bytes being enough. */
    if (c16) {
        fprintf(stderr,
            "FAIL: 16-byte buffer overrun -- libasound timespec is larger than "
            "expected; the alsa-rs htstamp buffer must grow\n");
        return 1;
    }
    printf("PASS: t64 overflow reproduced with 8 bytes, prevented with 16 bytes\n");
    return 0;
}
