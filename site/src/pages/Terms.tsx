import { Link } from "react-router-dom";

const updated = "2 October 2026";

export default function Terms() {
  return (
    <div className="mx-auto max-w-6xl px-6">
      <article className="prose-doc max-w-3xl py-12 lg:py-16">
        <p className="label text-accent">Legal · last updated {updated}</p>
        <h1>Terms of use</h1>
        <p>opcode is software, not a service. There is no account to create, no subscription to accept, and no operator on the other end. These terms describe what you are agreeing to when you use this website and the opcode binary.</p>

        <h2>The licence</h2>
        <p>opcode is released under the MIT Licence. You may use, copy, modify, merge, publish, distribute, sublicense and sell copies of it, subject to the licence text and the copyright notice it carries. The full text is in <a href="https://github.com/Chmgx81/opcode/blob/main/LICENSE">LICENSE</a> in the repository.</p>
        <p>This website's source lives in the same repository under the same licence.</p>

        <h2>No warranty</h2>
        <p>The software is provided "as is", without warranty of any kind, express or implied, including the warranties of merchantability, fitness for a particular purpose and non-infringement. In plain terms: it is built carefully and tested, but nobody promises it will do anything in particular, and nobody is liable if it does not.</p>
        <p>This matters most where opcode touches your files and runs your commands. Read the <Link className="link" to="/docs/security">security documentation</Link> before you rely on the sandbox for anything you cannot afford to lose. Keep backups. Review diffs before approving them. You are responsible for what you approve.</p>

        <h2>What you send to model providers</h2>
        <p>opcode is a client. When you use it, the conversation, the file contents it has read, and the tool output it has gathered are sent to whichever model provider you configured, under that provider's terms and pricing. You need the right to send that material, and you are responsible for choosing a provider whose terms permit it. We are not a party to those exchanges and see none of them.</p>

        <h2>Your keys and your data</h2>
        <p>Keys you store in <code>~/.opcode/auth.json</code> are yours. They stay on your machine, they are never transmitted to us, and there is no mechanism in opcode by which they could be. Sessions, configuration and skills in the same directory are likewise yours alone.</p>

        <h2>Acceptable use</h2>
        <p>Use opcode on code and systems you are permitted to work on. The permission gate is a safety mechanism, not a legal one: choosing <code>full-auto</code> or <code>--trust</code> removes prompts, not responsibility. Automation that runs against third-party services remains subject to those services' rules.</p>

        <h2>Contributions</h2>
        <p>By contributing to the repository you agree that your contribution is licensed under the same MIT Licence, and that you have the right to submit it.</p>

        <h2>Changes to these terms</h2>
        <p>These terms change only when the project changes what it offers. The repository history is the record. Continued use after a change means acceptance of the change; if a change ever conflicted with the MIT Licence, the licence wins.</p>

        <h2>Contact</h2>
        <p>Open an issue on <a href="https://github.com/Chmgx81/opcode">github.com/Chmgx81/opcode</a>. Security matters go through <a href="https://github.com/Chmgx81/opcode/security/advisories">GitHub security advisories</a>.</p>

        <p className="mt-10 border-t border-rule pt-4 text-[0.8125rem] text-ink-3">
          Also read the{" "}
          <Link className="text-ink underline decoration-accent decoration-1 underline-offset-4 hover:text-accent" to="/privacy">
            privacy policy
          </Link>
          .
        </p>
      </article>
    </div>
  );
}
