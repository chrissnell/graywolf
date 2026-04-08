# APRS Symbols

The PNG sprite sheets and `symbols.csv` index in this directory are vendored
from Heikki Hannikainen (OH7LZB)'s open APRS symbol set:

- Sprites: https://github.com/hessu/aprs-symbols
- Index:   https://github.com/hessu/aprs-symbol-index

## Files

- `aprs-symbols-24-0.png`     primary table (`/`), 24px cells, 16x6 grid
- `aprs-symbols-24-0@2x.png`  same, 48px cells (HiDPI)
- `aprs-symbols-24-1.png`     alternate table (`\`), 24px cells
- `aprs-symbols-24-1@2x.png`  same, 48px cells (HiDPI)
- `aprs-symbols-24-2.png`     overlay characters (0-9, A-Z), 24px cells
- `aprs-symbols-24-2@2x.png`  same, 48px cells (HiDPI)
- `symbols.csv`               tab-separated index: CODE, DSTCALL, DESCRIPTION, ROTATEDEG

## Sprite layout

Each sheet is 16 columns x 6 rows. The cell for an APRS symbol character `c`
is at:

    col = (c - 0x20) % 16
    row = (c - 0x20) / 16

i.e. cell (0,0) is the unused space character (0x20); the printable APRS range
is 0x21 (`!`) through 0x7E (`~`).

## Licensing

The symbols carry mixed licensing — most are CC BY-SA 2.0 or unknown
("VEC-OH7LZB"). A handful (Apple, Microsoft, Kenwood logos) are
trademark-restricted. See the upstream `COPYRIGHT.md` for the per-symbol
breakdown:

  https://github.com/hessu/aprs-symbols/blob/master/COPYRIGHT.md

graywolf bundles these as a UI convenience for selecting beacon symbols.
No modification has been made to the upstream files.
