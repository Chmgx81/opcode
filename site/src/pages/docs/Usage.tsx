import DocsShell from "@/components/DocsShell";

const keys: [string, string][] = [
  ["enter", "Send. Mid-turn, it steers at the next round boundary."],
  ["ctrl+j / shift+enter", "Newline."],
  ["alt+enter", "Queue a follow-up while a turn is running."],
  ["ctrl+r", "Expand or collapse results and thinking. When idle, it opens the transcript pager."],
  ["ctrl+o", "Transcript pager: the whole conversation, scrollable."],
  ["ctrl+v", "Attach the clipboard image (png, jpeg, gif, webp)."],
  ["ctrl+e", "Edit the draft in $VISUAL or $EDITOR."],
  ["tab / shift+tab", "Cycle the permission mode."],
  ["alt+. / alt+,", "Reasoning effort up and down."],
  ["esc", "Interrupt the turn, stop a running command, or close exactly one open thing."],
  ["ctrl+c", "Press twice to exit. Works inside dialogs and pickers too."],
  ["?", "Help sheet, from an empty idle composer."],
  ["/", "Command palette."],
  ["@", "File picker, to attach a file's contents."],
  ["!", "Shell mode: enter runs it directly, with no model round trip."],
];

const commands = [
  "/help", "/models", "/model", "/mode", "/sessions", "/skills", "/mcp", "/theme",
  "/diff", "/login", "/logout", "/doctor", "/update", "/exit", "/quit",
];

export default function Usage() {
  return (
    <DocsShell source="https://github.com/Chmgx81/opcode/blob/main/docs/specs/tui-spec.md">
      <h1>Usage</h1>
      <h2>Modes</h2>
      <p>Three permission postures, cycled with <kbd>tab</kbd> and <kbd>shift</kbd>+<kbd>tab</kbd>, or picked with <code>/mode</code>. The footer shows the current one: <code>⏸ plan</code>, <code>› build</code>, <code>⏵⏵ full-auto</code>.</p>
      <table>
        <thead><tr><th>Mode</th><th>What runs without asking</th></tr></thead>
        <tbody>
          <tr><td>plan</td><td>Read-only tools. Every write, command or fetch proposes a plan you approve.</td></tr>
          <tr><td>build (default)</td><td>Sandboxed commands and in-tree writes. Anything else asks.</td></tr>
          <tr><td>full-auto</td><td>Everything. The sandbox still confines writes to the project directory, <code>/tmp</code> and dev caches.</td></tr>
        </tbody>
      </table>
      <h2>Permission dialogs</h2>
      <p>An action outside the mode's bounds opens a dialog: the literal command in plain words, the tier it belongs to, and three options. <strong>Yes</strong>. <strong>Yes, and don't ask again for</strong>, a session-scoped prefix rule that is never wider than what the dialog showed. And <strong>No</strong>, which is what the selection starts on. For 400ms after a dialog opens, keystrokes are swallowed, so a fast typist's stray <code>y</code> cannot answer an approval they never read.</p>
      <h2>Keys</h2>
      <table>
        <thead><tr><th>Key</th><th>Action</th></tr></thead>
        <tbody>
          {keys.map(([k, a]) => (
            <tr key={k}><td><kbd>{k}</kbd></td><td>{a}</td></tr>
          ))}
        </tbody>
      </table>
      <h2>Commands</h2>
      <p>{commands.map((c, i) => (
        <span key={c}>
          <code>{c}</code>{i < commands.length - 1 ? " " : ""}
        </span>
      ))}</p>
      <p>An unknown command errors in place with the closest match. It never becomes a billed model turn.</p>
      <h2>Sessions</h2>
      <p>The session file rewrites atomically at every turn boundary, so a crash loses at most the turn in flight. <code>opcode --continue</code> resumes the newest readable session, skipping and naming a damaged newest file. <code>/sessions</code> lists them all, and resuming grows that session's own file.</p>
      <p>Compaction keeps the context bounded when <code>context_window</code> is configured. The working line reads it as <code>context 62%</code>.</p>
    </DocsShell>
  );
}
