#!/usr/bin/env python3
"""Run the example playbook with preview and signal-forwarding conventions."""

from __future__ import annotations

import argparse
import os
from pathlib import Path
import signal
import subprocess
import sys
from typing import Sequence


_FORWARDED_SIGNALS = (signal.SIGHUP, signal.SIGINT, signal.SIGQUIT, signal.SIGTERM)


def _parse_arguments(arguments: Sequence[str]) -> argparse.Namespace:
    parser = argparse.ArgumentParser(
        description="Run an Ansible playbook through the Slipramp example launcher.",
    )
    parser.add_argument("mode", choices=("preview", "apply"))
    parser.add_argument("playbook", help="repository-relative playbook path")
    parser.add_argument("--limit", help="Ansible host or group limit")

    return parser.parse_args(arguments)


def _resolve_playbook(repository: Path, value: str) -> Path:
    requested = Path(value)
    if requested.is_absolute():
        raise ValueError("playbook path must be relative to the target directory")

    resolved = (repository / requested).resolve()
    if resolved != repository and repository not in resolved.parents:
        raise ValueError("playbook path must stay inside the target directory")
    if not resolved.is_file():
        raise ValueError(f"playbook does not exist: {value}")

    return resolved


def _run(command: Sequence[str]) -> int:
    child: subprocess.Popen[bytes] | None = None
    pending_signal: int | None = None

    def forward(received: int, _frame: object) -> None:
        nonlocal pending_signal
        if pending_signal is None:
            pending_signal = received
        if child is None or child.poll() is not None:
            return

        try:
            os.killpg(child.pid, received)
        except ProcessLookupError:
            pass

    previous_handlers = {received: signal.signal(received, forward) for received in _FORWARDED_SIGNALS}
    try:
        child = subprocess.Popen(command, start_new_session=True)
        if pending_signal is not None and child.poll() is None:
            os.killpg(child.pid, pending_signal)

        return_code = child.wait()
    finally:
        for received, handler in previous_handlers.items():
            signal.signal(received, handler)

    if return_code < 0:
        return 128 - return_code

    return return_code


def main(arguments: Sequence[str]) -> int:
    parsed = _parse_arguments(arguments)
    repository = Path.cwd().resolve()
    try:
        playbook = _resolve_playbook(repository, parsed.playbook)
    except ValueError as error:
        print(error, file=sys.stderr)
        return 64

    command = [
        "ansible-playbook",
        "--inventory",
        str(repository / "inventory" / "hosts.yml"),
        str(playbook.relative_to(repository)),
    ]
    if parsed.mode == "preview":
        command.extend(("--check", "--diff"))
    if parsed.limit:
        command.extend(("--limit", parsed.limit))

    return _run(command)


if __name__ == "__main__":
    raise SystemExit(main(sys.argv[1:]))
