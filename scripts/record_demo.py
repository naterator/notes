"""Record a real Notes TUI-then-CLI session as an asciicast v2 file.

Usage: python3 scripts/record_demo.py /path/to/notes assets/demo.cast
Only temporary sample notes/config are used. No terminal output is fabricated.
"""

import argparse
import codecs
import fcntl
import json
import os
import pty
import re
import select
import struct
import subprocess
import tempfile
import termios
import time
from pathlib import Path


class Recording:
    def __init__(self, binary: Path, base: Path):
        self.events = []
        self.output = bytearray()
        self.decoder = codecs.getincrementaldecoder("utf-8")("replace")
        self.started = time.monotonic()
        self.timestamp = int(time.time())
        self.master, slave = pty.openpty()
        fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 28, 96, 0, 0))
        (base / "bin").mkdir()
        (base / "bin" / "notes").symlink_to(binary)
        config = base / "config.toml"
        config.write_text('[git]\nauto_sync = false\n[tui]\neditor_mode = "vim"\nautosave_delay = "10m"\n')
        env = os.environ.copy()
        for key in ("NOTES_CONFIG", "NOTES_REPO", "NOTES_THEME", "NOTES_COLOR", "NO_COLOR", "EDITOR", "VISUAL", "PROMPT_COMMAND"):
            env.pop(key, None)
        env.update({
            "NOTES_CONFIG": str(config), "NOTES_REPO": str(base / "repo"),
            "NOTES_THEME": "catppuccin-mocha", "NOTES_COLOR": "always",
            "TERM": "xterm-256color", "COLORTERM": "truecolor",
            "PATH": str(base / "bin") + os.pathsep + env["PATH"],
            "HISTFILE": os.devnull,
            "XDG_STATE_HOME": str(base / "state"),
            "XDG_CACHE_HOME": str(base / "cache"),
            "BASH_SILENCE_DEPRECATION_WARNING": "1",
            "PS1": "\\[\\e[38;2;203;166;247m\\]❯ \\[\\e[0m\\]",
        })

        def controlling_terminal():
            os.setsid()
            fcntl.ioctl(0, termios.TIOCSCTTY, 0)

        self.proc = subprocess.Popen(
            ["/bin/bash", "--noprofile", "--norc", "-i"],
            cwd=base, env=env, stdin=slave, stdout=slave, stderr=slave,
            preexec_fn=controlling_terminal,
        )
        os.close(slave)

    def read(self, seconds=0.04):
        if select.select([self.master], [], [], seconds)[0]:
            try:
                chunk = os.read(self.master, 65536)
            except OSError:
                return
            if chunk:
                self.output.extend(chunk)
                text = self.decoder.decode(chunk)
                if text:
                    self.events.append([round(time.monotonic() - self.started, 4), "o", text])

    def hold(self, seconds):
        until = time.monotonic() + seconds
        while time.monotonic() < until:
            self.read(min(0.04, max(0, until - time.monotonic())))

    def send(self, data):
        self.events.append([round(time.monotonic() - self.started, 4), "i", data.decode("utf-8")])
        os.write(self.master, data)

    def wait(self, marker, start=0):
        until = time.monotonic() + 10
        while time.monotonic() < until:
            self.read()
            text = re.sub(rb"\x1b\[[0-?]*[ -/]*[@-~]", b"", bytes(self.output[start:]))
            if marker.encode() in text:
                self.hold(0.2)
                return
        raise RuntimeError(f"Missing {marker!r}: {self.output[-900:]!r}")

    def key(self, key, expected=None):
        start = len(self.output)
        self.send(key)
        if expected:
            self.wait(expected, start)
        else:
            self.hold(0.2)

    def type(self, text, delay=0.055):
        for character in text:
            self.send(("\r" if character == "\n" else character).encode())
            self.hold(delay)

    def command(self, text, pause=1.7):
        self.type(text, 0.045)
        self.key(b"\r", "❯ ")
        self.hold(pause)

    def new_note(self, title, tags):
        self.key(b"\x07")
        self.key(b"n", "New note")
        self.hold(0.8)
        self.type(title)
        self.key(b"\r")
        self.key(b"\x15")
        self.type("personal")
        self.key(b"\r")
        self.type(tags)
        self.hold(0.8)
        slug = title.lower().replace(" ", "-")
        self.key(b"\r", slug + ".md [FOCUS]")
        self.hold(0.6)

    def append(self, text):
        self.key(b"G")
        self.key(b"o")
        self.type(text, 0.04)
        self.key(b"\x1b")
        self.key(b"\x07")
        self.key(b"s")
        self.hold(1.7)

    def close(self):
        if self.proc.poll() is None:
            self.proc.kill()
            self.proc.wait(timeout=5)
        os.close(self.master)

    def save(self, path):
        header = {
            "version": 2, "width": 96, "height": 28,
            "timestamp": self.timestamp, "title": "Notes: TUI, then CLI",
            "env": {"TERM": "xterm-256color", "SHELL": "/bin/bash"},
        }
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text("\n".join(json.dumps(row, ensure_ascii=False) for row in [header, *self.events]) + "\n")


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("binary", type=Path)
    parser.add_argument("output", type=Path)
    args = parser.parse_args()
    binary = args.binary.resolve(strict=True)
    with tempfile.TemporaryDirectory(prefix="notes-demo-") as temp:
        base = Path(temp)
        session = Recording(binary, base)
        try:
            session.wait("❯ ")
            session.type("notes tui")
            session.key(b"\r", "[FOCUS]")
            session.hold(1)
            session.new_note("Weekend plans", "weekend ideas")
            session.append("- Pick a trail\n- Pack lunch\n- Check the weather")
            session.new_note("Gear checklist", "outdoors gear")
            session.append("- Water bottle\n- Lightweight jacket\n- First aid kit")
            session.key(b"\x07")
            session.key(b"a")
            session.type("Weekend")
            session.hold(1)
            session.key(b"\r", "weekend-plans.md [FOCUS]")
            session.append("- Invite a friend")
            session.hold(1)
            session.key(b"\x07")
            session.key(b"q", "❯ ")
            session.key(b"\x0c")
            session.command('notes new "Reading list" --category personal --tag books')
            session.command("notes list --relative")
            session.command('notes show "$(notes find \'Weekend plans\')"', pause=2.5)
            session.key(b"\x0c")
            session.command("notes find jacket --relative")
            session.command("notes tags")
            session.command('notes journal add "Planned the weekend" --section activities --tag weekend --date 2026-09-29')
            session.hold(2)
            weekend = list((base / "repo").rglob("*weekend-plans.md"))
            gear = list((base / "repo").rglob("*gear-checklist.md"))
            assert len(weekend) == len(gear) == 1
            assert "Invite a friend" in weekend[0].read_text()
            assert "Lightweight jacket" in gear[0].read_text()
            assert "Planned the weekend" in (base / "repo/journal/2026/09/2026-09-29.md").read_text()
            session.save(args.output)
            print(f"Recorded {session.events[-1][0]:.1f}s of actual terminal output to {args.output}")
        finally:
            session.close()


if __name__ == "__main__":
    main()
