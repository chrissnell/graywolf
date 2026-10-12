#!/usr/bin/env python3
"""Expose a Peet Bros Ultimeter test stream on a pseudo-terminal.

The script prints the slave PTY path for Graywolf's Peet Bros serial setting.
It emits Data Logger records by default; use --mode packet for $ULTW records.
The values are intentionally synthetic and intended only for development.
"""

import argparse
import os
import pty
import signal
import sys
import time
import tty


DATA_LOGGER = "!!0032004002A80010279402AC022601F40100020000030028"
PACKET = "$ULTW0078004002A80010279400000000000002260100020000030028"


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--mode", choices=("logger", "packet"), default="logger")
    parser.add_argument("--interval", type=float, default=1.0)
    args = parser.parse_args()
    if args.interval <= 0:
        parser.error("--interval must be positive")

    master_fd, slave_fd = pty.openpty()
    tty.setraw(slave_fd)
    slave_path = os.ttyname(slave_fd)
    record = DATA_LOGGER if args.mode == "logger" else PACKET

    print(slave_path, flush=True)

    stopping = False

    def stop(_signum, _frame):
        nonlocal stopping
        stopping = True

    signal.signal(signal.SIGINT, stop)
    signal.signal(signal.SIGTERM, stop)

    try:
        while not stopping:
            os.write(master_fd, (record + "\r\n").encode("ascii"))
            time.sleep(args.interval)
    except BrokenPipeError:
        pass
    finally:
        os.close(slave_fd)
        os.close(master_fd)
    return 0


if __name__ == "__main__":
    sys.exit(main())
