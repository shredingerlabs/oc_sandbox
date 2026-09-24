#!/usr/bin/env python3
"""Run a command in a PTY with deterministic keyboard input."""

import argparse
import fcntl
import os
import pty
import select
import signal
import struct
import sys
import termios
import time


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument("--input", default="")
    parser.add_argument("--submit", default="")
    parser.add_argument("--timeout", type=float, default=5)
    parser.add_argument("command", nargs=argparse.REMAINDER)
    args = parser.parse_args()
    if args.command[:1] == ["--"]:
        args.command = args.command[1:]
    if not args.command:
        parser.error("a command is required")

    pid, fd = pty.fork()
    if pid == 0:
        fcntl.ioctl(0, termios.TIOCSWINSZ, struct.pack("HHHH", 30, 100, 0, 0))
        env = os.environ.copy()
        env.setdefault("TERM", "xterm")
        env.setdefault("COLUMNS", "100")
        env.setdefault("LINES", "30")
        os.execvpe(args.command[0], args.command, env)

    output = bytearray()
    input_chunks = args.input.split("|")
    if args.submit:
        input_chunks.append(args.submit)
    input_index = 0
    input_due = time.monotonic() + 0.2
    deadline = time.monotonic() + args.timeout
    status = None
    cursor_queries = 0
    try:
        while time.monotonic() < deadline:
            readable, _, _ = select.select([fd], [], [], 0.05)
            if readable:
                try:
                    chunk = os.read(fd, 4096)
                except OSError:
                    break
                if not chunk:
                    break
                output.extend(chunk)
                query_count = output.count(b"\x1b[6n")
                if query_count > cursor_queries:
                    os.write(fd, b"\x1b[1;1R")
                    cursor_queries = query_count
            if input_index < len(input_chunks) and time.monotonic() >= input_due:
                os.write(fd, input_chunks[input_index].encode())
                input_index += 1
                input_due = time.monotonic() + 0.2
            try:
                waited, status = os.waitpid(pid, os.WNOHANG)
            except ChildProcessError:
                break
            if waited == pid:
                break
            status = None
        else:
            try:
                os.killpg(pid, signal.SIGTERM)
            except OSError:
                pass
            _, status = os.waitpid(pid, 0)
            sys.stdout.buffer.write(output)
            sys.stdout.flush()
            print("PTY command timed out", file=sys.stderr)
            return 124
    finally:
        os.close(fd)

    sys.stdout.buffer.write(output)
    sys.stdout.flush()
    if status is None:
        _, status = os.waitpid(pid, 0)
    return os.waitstatus_to_exitcode(status)


if __name__ == "__main__":
    raise SystemExit(main())
