#!/usr/bin/env bash
# Emit ~10 KiB to stdout to exercise cappedWriter at cmdOutputCap (4 KiB).
for i in $(seq 1 256); do
  printf 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA\n'
done
exit 0
