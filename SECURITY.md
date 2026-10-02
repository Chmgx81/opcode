# Security policy

## Reporting a vulnerability

Please report security issues **privately** through GitHub's security
advisories: <https://github.com/Chmgx81/opcode/security/advisories/new>
("Report a vulnerability" on the repository's Security tab). Do not open
a public issue for something exploitable.

Include what you can: the opcode version (`opcode --version`), your OS and
architecture, and steps to reproduce. You will get an acknowledgement
through the advisory thread; fixes ship as a new patch release.

## Supported versions

Only the **latest release** receives security fixes. `opcode update`
moves you to it. opcode checks once a day at startup (cached, silent
when offline) and tells you when a newer release exists — `/doctor`
shows the same check. Opt out with `"update_checks": false` in
config.json or `OPCODE_NO_UPDATE_CHECK=1`.

## Threat model

opcode is a coding agent: by design it reads files and runs shell
commands on your machine at a model's request, under a permission
system you control. What that does and does not protect:

- **The permission gate is the primary control.** In `plan` mode every
  state-changing call asks you first. In `build` mode, calls the
  sandbox bounds (confined shell commands, file writes inside the
  project and temp dirs) run without prompting and anything that
  escapes the bound asks. `full-auto` runs everything without
  prompting (calls are logged). Pick the mode that matches how much
  you trust the model and the project you are in.
- **The sandbox is defense in depth, not a security boundary.** On
  Linux, shell commands run under a Landlock ruleset that confines
  *writes* to the project directory, the temp dir, and a few dev
  caches. Reads and execution are unrestricted, so a confined command
  can still read your files. A model can also request `{"sandbox":
  false}`; in `plan` and `build` mode that goes through the approval
  dialog, in `full-auto` it does not. Where the kernel lacks Landlock, or on
  macOS and Windows, there is no sandbox and opcode says so.
- **Network blocking is narrower still.** The seccomp filter that
  denies `AF_INET`, `AF_INET6` and `AF_PACKET` sockets to sandboxed
  commands is written for **x86_64 Linux only**. On Linux arm64 the
  file confinement applies but the network stays open, and the
  startup status line does not claim otherwise. It does not stop
  unsandboxed commands, opcode's own connection to your model provider,
  or MCP servers you configure.
- **API keys** live in `~/.opcode/auth.json`, created with mode 0600
  inside a 0700 directory, and are never read from a project
  directory. opcode's file tools refuse to read that file in any
  permission mode, and known keys are redacted from the audit log and
  saved sessions. A shell command you approve can still read it, and
  any process running as your user can too.
- **Project content is untrusted input.** A repository can contain
  instructions aimed at the model (prompt injection). Project-level
  skills only load after you trust the project, and the trust
  fingerprint covers skill scripts, `SKILL.md` bodies, and their
  bundled references/assets — editing any of them re-asks.
- **The model's web fetches cannot reach your network.** `web_fetch`
  refuses loopback, private-LAN, link-local, and unspecified
  addresses (literal IPs) on every redirect hop, and asks first in
  `plan` and `build` modes. Hostnames that resolve into those ranges
  are not resolved by the check (DNS-rebinding TOCTOU) — the approval
  dialog is the control there, not the address filter.
  `OPCODE_ALLOW_LOCAL_FETCH=1` opts out for local dev servers.
- **Downloads are verified.** `install.sh` and `opcode update` check the
  archive's sha256 against the release's `checksums.txt` before
  running or installing anything, and refuse on mismatch. The checksum
  file comes from the same GitHub release as the archive, so this
  protects against corruption and a tampered mirror or proxy, **not**
  against someone who can publish to the repository's releases.
  Releases are not yet signed or attested.

## Out of scope

Behaviour a user explicitly enables (`full-auto`, `{"sandbox": false}`
approvals, a malicious MCP server you configured) is working as
designed, though clearer warnings are welcome as ordinary issues.
