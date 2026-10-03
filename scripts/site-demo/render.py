import os
import sys

import pyte

raw = open(sys.argv[1], "rb").read().decode("utf8", "replace")
cols = int(os.environ.get("CAPTURE_COLS", "100"))
rows = int(os.environ.get("CAPTURE_ROWS", "42"))
screen = pyte.Screen(cols, rows)
stream = pyte.Stream(screen)
stream.feed(raw)
for line in screen.display:
    print(line.rstrip())
