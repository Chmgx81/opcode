import { Link } from "react-router-dom";

const updated = "2 October 2026";

export default function Privacy() {
  return (
    <div className="mx-auto max-w-6xl px-6">
      <article className="prose-doc max-w-3xl py-12 lg:py-16">
        <p className="label text-accent">Legal · last updated {updated}</p>
        <h1>Privacy policy</h1>
        <p>The short version: this website sets no cookies and runs no analytics, and the opcode binary collects nothing about you. What follows is the long version, written so that each claim can be checked.</p>

        <h2>This website</h2>
        <ul>
          <li>No cookies. No local storage beyond what the page itself needs to render. No advertising, no tracking pixels, no third-party scripts, no tag manager.</li>
          <li>The fonts are served from this site's own origin, so loading the page does not announce you to a font CDN.</li>
          <li>We host no backend for this site. There is no form, no login, and no database to send anything to. We cannot see your visit, because nothing is reported back.</li>
          <li>The header reads the latest release number once, with a request to <code>api.github.com</code>. That request goes to GitHub, not to us, and only happens if JavaScript is enabled. GitHub's own handling of it is described in GitHub's privacy statement. If the request fails, the page shows a cached number instead.</li>
        </ul>
        <p>This site is served by GitHub Pages. Like any web host, GitHub records standard server logs, including your IP address and user agent, under its own privacy statement. We do not receive, keep, or read those logs.</p>

        <h2>The opcode binary</h2>
        <ul>
          <li>No telemetry of any kind. Opcode does not report your prompts, your code, your filenames, your usage, or your identity to us. There is no server of ours for it to talk to.</li>
          <li>One update check: on startup, at most once a day, capped at three seconds, and silent when offline. It asks GitHub for the latest release tag and compares it to the running version. Set <code>OPCODE_NO_UPDATE_CHECK=1</code> or <code>"update_checks": false</code> to switch it off.</li>
          <li><code>opcode update</code> downloads a release asset and its sha256 checksum when you run it, and nothing else runs on its own.</li>
          <li>Model requests go directly from your machine to the provider you configured, with your key. We are not in that path and never see it.</li>
          <li>Your keys stay in <code>~/.opcode/auth.json</code> at mode 0600 on your machine. Your sessions stay in <code>~/.opcode/sessions/</code>. Neither is ever written into a project directory unless you ask for it.</li>
        </ul>

        <h2>Third parties you choose to involve</h2>
        <p>Model providers, MCP servers and any custom endpoint you configure are outside this policy. They receive whatever you direct opcode to send them, under their own terms. The sandbox, the permission gate and the secret redactor exist to keep that surface narrow; the full posture is in the <a href="https://github.com/Chmgx81/opcode/blob/main/SECURITY.md">security documentation</a>.</p>

        <h2>Changes</h2>
        <p>This policy changes only when the site or the binary changes what it does. The commit history of this repository is the record, and a material change is noted in the release notes.</p>

        <h2>Questions</h2>
        <p>Open an issue on <a href="https://github.com/Chmgx81/opcode">github.com/Chmgx81/opcode</a>. Security matters go through <a href="https://github.com/Chmgx81/opcode/security/advisories">GitHub security advisories</a> instead.</p>

        <p className="mt-10 border-t border-rule pt-4 text-[0.8125rem] text-ink-3">
          Also read the{" "}
          <Link className="text-ink underline decoration-accent decoration-1 underline-offset-4 hover:text-accent" to="/terms">
            terms of use
          </Link>
          .
        </p>
      </article>
    </div>
  );
}
