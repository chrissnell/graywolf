# SauceCodePro Nerd Font (vendored)

This directory will hold the woff2-subset, self-hosted SauceCodePro Nerd
Font used by the AX.25 Terminal route.

See `VERSION.txt` for the planned upstream pin and the build pipeline,
and `saucecodepro-nerd-font.css` for the face declarations. The `.css`
file falls back to system monospace via `local()` because the woff2
binaries have not been vendored yet.

License files (`OFL.txt`, `LICENSE-NerdFonts.txt`) will land alongside
the woff2 files when they are vendored.
