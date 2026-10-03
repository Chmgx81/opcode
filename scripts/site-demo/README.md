# site-demo: recording the site's figures

The site shows three figures and two code blocks. All five are text the
real `opcode` binary printed. This directory is how they were produced
and how to produce them again.

## What is real and what is not

**Real:** the binary, the interface, the sandbox line, the permission
dialog, the diff, the `--json` event stream. Built from the `v0.6.0` tag
with the same `-ldflags` the release workflow uses, so the version line
in each figure is true.

**Not real:** the model. There is no API key in this repository and none
was used. `recording-rig.py` is a scripted OpenAI-compatible server on
`127.0.0.1` that replays a fixed sequence of responses. It exists so a
real session can be driven end to end: opcode talks to it exactly as it
talks to any provider, and everything opcode then draws or prints is
genuine.

If a figure is ever questioned, re-run `capture.sh` and diff the output
against `out/`.

## Reproducing

```sh
python3 -m venv .venv && .venv/bin/pip install pyte   # for render.py only
bash scripts/site-demo/capture.sh
```

`capture.sh` expects a worktree of the `v0.6.0` tag at
`/tmp/opcode-demo-src`, builds `opcode` from it, records at 100x42 (48
rows for the diff figure), and writes rendered screens to `out/`.

## Files

| | |
|---|---|
| `recording-rig.py` | the scripted model, `python3 recording-rig.py <port> <approve\|diff>` |
| `pty-capture.py` | runs a program under a pseudo-terminal, answers its probes, injects keystrokes on a schedule, dumps raw output |
| `render.py` | replays raw output through a terminal emulator (`pyte`) to get the final screen |
| `capture.sh` | the three recordings plus the headless event stream and `--help` |
| `out/` | the rendered screens the site ships |

`pty-capture.py` sets the pseudo-terminal's window size explicitly. A
size of 0x0 leaves bubbletea with nowhere to draw overlays, which is why
the approval dialog does not appear until you set it.
