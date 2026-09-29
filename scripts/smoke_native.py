"""Exercise a native binary end to end against a temporary Git remote and PTY."""

import fcntl
import json
import os
import pty
import re
import select
import signal
import struct
import subprocess
import sys
import tempfile
import termios
import time
from pathlib import Path

SETTINGS_HOME = "Settings · Esc close".encode()


def terminal_output(argv: list[str], env: dict) -> bytes:
    master, slave = pty.openpty()
    proc = subprocess.Popen(argv, stdin=subprocess.DEVNULL, stdout=slave, stderr=slave, env=env)
    os.close(slave)
    output = bytearray()
    try:
        while True:
            try:
                chunk = os.read(master, 65536)
            except OSError:
                break
            if not chunk:
                break
            output.extend(chunk)
        if proc.wait(timeout=10) != 0:
            raise RuntimeError(f"terminal command failed: {output[-600:]!r}")
    finally:
        if proc.poll() is None:
            proc.kill()
            proc.wait()
        os.close(master)
    return bytes(output).replace(b"\r\n", b"\n")


def drive_tui(binary: str, config: Path, repo: Path, env: dict, keys: tuple[bytes | tuple[bytes, bytes], ...], probe_resize: bool = False, command: str = "tui", extra_flags: tuple[str, ...] = ()) -> bytes:
    master, slave = pty.openpty()
    fcntl.ioctl(slave, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
    proc = subprocess.Popen(
        [binary, "--config", str(config), "--repo", str(repo), *extra_flags, command, "smoke.md"],
        stdin=slave,
        stdout=slave,
        stderr=slave,
        env=env,
    )
    os.close(slave)
    output = bytearray()
    deadline = time.monotonic() + 10
    try:
        while b"[FOCUS]" not in output and time.monotonic() < deadline:
            if select.select([master], [], [], 0.2)[0]:
                try:
                    output.extend(os.read(master, 65536))
                except OSError:
                    break
        if b"[FOCUS]" not in output:
            raise RuntimeError("TUI did not show the focused pane")
        if b"smoke.md [FOCUS]" not in output:
            raise RuntimeError(f"TUI did not focus the specified note: {output[-600:]!r}")
        if probe_resize:
            fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 10, 70, 0, 0))
            os.kill(proc.pid, signal.SIGWINCH)
            deadline = time.monotonic() + 3
            while b"Resize terminal" not in output and time.monotonic() < deadline:
                if select.select([master], [], [], 0.1)[0]:
                    output.extend(os.read(master, 65536))
            if b"Resize terminal" not in output:
                raise RuntimeError(f"TUI did not show the small-terminal hint: {output[-800:]!r}")
            fcntl.ioctl(master, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
            os.kill(proc.pid, signal.SIGWINCH)
            time.sleep(0.2)
        for action in keys:
            key, expected = action if isinstance(action, tuple) else (action, None)
            start = len(output)
            os.write(master, key)
            # Async config reads/writes must finish before sending the next key.
            # Match fresh terminal output rather than trusting a fixed CI delay.
            until = time.monotonic() + (10 if expected else 0.15)
            matched = False
            while time.monotonic() < until:
                if select.select([master], [], [], 0.03)[0]:
                    try:
                        output.extend(os.read(master, 65536))
                    except OSError:
                        break
                if expected:
                    plain = re.sub(rb"\x1b\[[0-?]*[ -/]*[@-~]", b"", bytes(output[start:]))
                    if expected in plain:
                        matched = True
                        break
            if expected and not matched:
                raise RuntimeError(f"TUI did not reach {expected!r} after {key!r}; tail {output[-800:]!r}")
        deadline = time.monotonic() + 8
        while proc.poll() is None and time.monotonic() < deadline:
            if select.select([master], [], [], 0.2)[0]:
                try:
                    output.extend(os.read(master, 65536))
                except OSError:
                    break
        try:
            returncode = proc.wait(timeout=max(0.2, deadline - time.monotonic()))
        except subprocess.TimeoutExpired as exc:
            raise RuntimeError(f"TUI did not quit; output tail: {output[-600:]!r}") from exc
        if returncode != 0:
            raise RuntimeError(f"TUI exited with {proc.returncode}")
    finally:
        if proc.poll() is None:
            proc.kill()
            proc.wait()
        os.close(master)
    return bytes(output)


def main() -> int:
    binary = str(Path(sys.argv[1]).resolve())
    with tempfile.TemporaryDirectory(prefix="notes-smoke-") as temp:
        base = Path(temp)
        config = base / "config.toml"
        editor = base / "editor.py"
        editor.write_text('import pathlib, sys\np = pathlib.Path(sys.argv[-1])\np.write_text(p.read_text() + "\\nExternal editor #edited\\n")\n')
        config.write_text('editor = ' + json.dumps([sys.executable, str(editor)]) + '\n[git]\nauto_sync = false\n[tui]\neditor_mode = "vim"\n')
        repo = base / "repo"
        remote = base / "remote.git"
        env = os.environ.copy()
        for key in ("NOTES_CONFIG", "NOTES_REPO", "NOTES_THEME", "NOTES_COLOR"):
            env.pop(key, None)
        (base / "home").mkdir()
        env["HOME"] = str(base / "home")
        env["XDG_CONFIG_HOME"] = str(base / "config-home")
        env["XDG_STATE_HOME"] = str(base / "state")
        env["NO_COLOR"] = "1"
        env["TERM"] = "xterm-256color"
        env["GIT_CONFIG_NOSYSTEM"] = "1"
        env["GIT_CONFIG_GLOBAL"] = os.devnull
        def git(*args: str) -> bytes:
            return subprocess.run(["git", *args], check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env).stdout

        def notes(*args: str) -> bytes:
            return subprocess.run([binary, "--config", str(config), "--repo", str(repo), *args], check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, env=env).stdout

        pipe_repo = base / "pipe-repo"
        pipe_repo.mkdir()
        (pipe_repo / "large.md").write_bytes(b"# Large\n" + b"x" * (512 << 10))
        producer = subprocess.Popen(
            [binary, "--config", str(config), "--repo", str(pipe_repo), "show", "large.md"],
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            env=env,
        )
        producer.stdout.read(1)
        producer.stdout.close()
        pipe_error = producer.stderr.read()
        if producer.wait(timeout=10) != 0 or pipe_error:
            raise RuntimeError(f"closed-pipe output failed: {producer.returncode}, {pipe_error[:200]!r}")

        git("init", "--bare", "-b", "main", str(remote))
        git("init", "-b", "main", str(repo))
        git("-C", str(repo), "remote", "add", "origin", str(remote))
        git("-C", str(repo), "config", "user.name", "Notes Smoke")
        git("-C", str(repo), "config", "user.email", "notes-smoke@example.invalid")
        version = json.loads(subprocess.run([binary, "version", "--json"], check=True, stdout=subprocess.PIPE, env=env).stdout)
        if env.get("RELEASE_TAG") and version.get("version") != env["RELEASE_TAG"]:
            raise RuntimeError(f"binary version mismatch: {version!r}")
        notes("config", "render")
        path = Path(notes("new", "Smoke", "--path", "smoke.md", "--tag", "demo").decode().strip())
        if not path.is_file() or b"# Smoke" not in notes("show", "smoke.md"):
            raise RuntimeError("note creation or show failed")
        if notes("list") != (str(path) + "\n").encode():
            raise RuntimeError("piped list output changed")
        listed = terminal_output([binary, "--config", str(config), "--repo", str(repo), "l"], env)
        if listed != ("base: " + str(repo) + "\n/smoke.md\n").encode():
            raise RuntimeError(f"interactive list output: {listed!r}")
        notes("journal", "add", "Completed action", "--date", "2026-09-28", "--section", "actions")
        if b"[ ] Completed action" not in notes("show", "journal/2026/09/2026-09-28.md"):
            raise RuntimeError("journal entry was not saved")
        notes("edit", "smoke.md")
        if b"External editor #edited" not in notes("show", "smoke.md"):
            raise RuntimeError("external editor change was not saved")
        tui_config = base / "tui-config.toml"
        tui_config.write_text('[git]\nauto_sync = false\n[tui]\neditor_mode = "vim"\n')
        no_editor_env = env.copy()
        no_editor_env.pop("EDITOR", None)
        no_editor_env.pop("VISUAL", None)
        drive_tui(binary, tui_config, repo, no_editor_env, (b"\x07", b"q"), command="edit")
        output = drive_tui(binary, config, repo, env, (b"\x07", b"e", b"A", b" via TUI", b"\x1b", b"\x07", b"s", b"\x07", b"q"))
        saved = notes("show", "smoke.md")
        if b"via TUI" not in saved:
            raise RuntimeError(f"TUI save did not persist: {saved[:150]!r}; terminal tail: {output[-600:]!r}")
        notes("config", "set", "tui.editor_mode", "traditional")
        output = drive_tui(binary, config, repo, env, (b"\x07", b"e", b"\x05", b" traditional", b"\x1b", b"\x07", b"s", b"\x07", b"q"))
        saved = notes("show", "smoke.md")
        if b"traditional" not in saved:
            raise RuntimeError(f"traditional TUI save did not persist: {saved[:150]!r}; terminal tail: {output[-600:]!r}")
        notes("config", "set", "tui.editor_mode", "vim")
        notes("config", "set", "theme", "catppuccin-latte")
        color_env = env.copy()
        color_env.pop("NO_COLOR", None)
        pasted = "? /: カフェ\n\t#pasted"
        output = drive_tui(
            binary,
            config,
            repo,
            color_env,
            (b"\x07", b"e", b"A", b"\x1b[200~" + pasted.encode() + b"\x1b[201~", b"\x1b", b"\x07", b"s", b"\x07", b"q"),
            probe_resize=True,
        )
        saved = notes("show", "smoke.md")
        if pasted.encode() not in saved:
            raise RuntimeError(f"Unicode bracketed paste did not persist: {saved[:150]!r}; terminal tail: {output[-600:]!r}")
        for mode in ("vim", "traditional"):
            notes("config", "set", "tui.editor_mode", mode)
            notes("config", "set", "theme", "catppuccin-mocha")
            entry = (b"i",) if mode == "vim" else ()
            output = drive_tui(binary, config, repo, color_env, entry + (
                b" settings before ", b"\x07", (b"c", b"Search settings"),
                b"theme", (b"\r", b"Preview:"), b"nord", (b"\r", SETTINGS_HOME),
                b"\x15", b"tui.autosave_delay", (b"\r", b"Value [FOCUS]"), b"\x15", b"2s", (b"\r", SETTINGS_HOME),
                b"\x15", b"default_category", (b"\r", b"Value [FOCUS]"), b"\x15", b"discarded", (b"\x1b", SETTINGS_HOME),
                b"\x15", b"theme", (b"\r", b"Preview:"), b"solarized", (b"\x1b", SETTINGS_HOME),
                (b"\x12", b"Remove the saved value"), (b"\r", SETTINGS_HOME),
                b"\x15", b"effective", (b"\r", b"Effective configuration"), (b"\x1b", SETTINGS_HOME), (b"\x1b", b"smoke.md [FOCUS]"),
                b" settings after ", b"\x1b", b"\x07", b"s", b"\x07", b"q",
            ), probe_resize=True)
            rendered = json.loads(notes("config", "render", "--json"))
            if rendered["theme"] != "catppuccin-mocha" or rendered["tui"]["autosave_delay"] != "2s" or rendered["default_category"] != "misc":
                raise RuntimeError(f"settings save/cancel/reset failed: {rendered!r}; tail {output[-800:]!r}")
            if "theme =" in config.read_text():
                raise RuntimeError("theme reset materialized a default")
            saved = notes("show", "smoke.md")
            if b"settings before" not in saved or b"settings after" not in saved:
                raise RuntimeError(f"settings lost editor entry: {saved[:300]!r}; tail {output[-800:]!r}")
        notes("config", "set", "tui.editor_mode", "vim")
        output = drive_tui(binary, config, repo, color_env, (
            b"\x07", (b"c", b"Search settings"), b"theme", (b"\r", b"Preview:"), b"nord", (b"\r", SETTINGS_HOME), (b"\x1b", b"smoke.md [FOCUS]"), b"\x07", b"q",
        ), extra_flags=("--theme", "gruvbox-dark", "--no-color"))
        if notes("config", "get", "theme").strip() != b"nord" or b"flag --theme" not in output:
            raise RuntimeError(f"settings launch override lost: {output[-800:]!r}")
        for theme in (
            "catppuccin-mocha", "catppuccin-frappe", "catppuccin-macchiato", "catppuccin-latte",
            "nord", "gruvbox-dark", "solarized-light", "midnight", "graphite", "ocean", "pine",
            "ember", "amethyst", "rose", "sand", "mint", "blueprint", "terminal",
        ):
            notes("config", "set", "theme", theme)
            drive_tui(binary, config, repo, color_env, (b"\x07", b"q"))
        notes("sync")
        if b"via TUI" not in git("--git-dir", str(remote), "show", "main:smoke.md") or b"traditional" not in git("--git-dir", str(remote), "show", "main:smoke.md"):
            raise RuntimeError("synced note did not reach origin")
        if b"Completed action" not in git("--git-dir", str(remote), "show", "main:journal/2026/09/2026-09-28.md"):
            raise RuntimeError("synced journal did not reach origin")
    print("native end-to-end smoke passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
