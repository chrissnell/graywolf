#!/usr/bin/env bash
# Exercise the production Linux serial PTT adapter without serial hardware.
# Requires the normal Rust build dependencies and a C compiler.
set -euo pipefail
cd "$(dirname "$0")/.."
sim_dir=$(mktemp -d)
trap 'rm -rf "$sim_dir"' EXIT
cc -Wall -Wextra -Werror -shared -fPIC scripts/testdata/serial_ptt_sim.c -ldl -o "$sim_dir/ioctl.so"
touch "$sim_dir/device"
# Linux TIOCM_CAR (0x40) | TIOCM_CTS (0x20); preserve these unrelated bits.
echo 96 > "$sim_dir/state"
GRAYWOLF_SERIAL_SIM_DEVICE="$sim_dir/device" \
GRAYWOLF_SERIAL_SIM_STATE="$sim_dir/state" \
GRAYWOLF_SERIAL_SIM_FAIL="$sim_dir/fail-next-set" \
LD_PRELOAD="$sim_dir/ioctl.so" \
cargo test -p graywolf-demod --lib \
  tx::ptt::tests::serial_unix_simulator_exercises_real_adapter -- --exact --ignored --nocapture
