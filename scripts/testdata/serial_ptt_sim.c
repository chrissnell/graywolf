// Linux modem-control register simulator for test_serial_ptt_sim.sh.
// Only ioctls on the explicitly named temporary device are intercepted.
// No serial hardware or radio is accessed.
#define _GNU_SOURCE
#include <dlfcn.h>
#include <errno.h>
#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <sys/ioctl.h>
#include <sys/stat.h>
#include <unistd.h>

static int bits = TIOCM_CAR | TIOCM_CTS;

int ioctl(int fd, unsigned long request, ...) {
    va_list args;
    va_start(args, request);
    void *arg = va_arg(args, void *);
    va_end(args);
    const char *device = getenv("GRAYWOLF_SERIAL_SIM_DEVICE");
    struct stat actual, expected;
    if (device && (request == TIOCMGET || request == TIOCMSET) &&
        fstat(fd, &actual) == 0 && stat(device, &expected) == 0 &&
        actual.st_dev == expected.st_dev && actual.st_ino == expected.st_ino) {
        if (request == TIOCMGET) {
            *(int *)arg = bits;
            return 0;
        }
        const char *fail = getenv("GRAYWOLF_SERIAL_SIM_FAIL");
        if (fail && unlink(fail) == 0) {
            errno = EIO; // one-shot recoverable write failure
            return -1;
        }
        bits = *(int *)arg;
        FILE *state = fopen(getenv("GRAYWOLF_SERIAL_SIM_STATE"), "w");
        if (!state) return -1;
        fprintf(state, "%d\n", bits);
        fclose(state);
        return 0;
    }
    int (*real_ioctl)(int, unsigned long, ...) = dlsym(RTLD_NEXT, "ioctl");
    return real_ioctl(fd, request, arg);
}
