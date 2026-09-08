"""Prepare small, bounded hard-link fixtures on the authorized Linux test host."""

import os
from pathlib import Path

fixture_root = Path("/opt/goby-fixtures").resolve(strict=True)
cancel_root = (fixture_root / "cancel").resolve()
source = Path(os.environ["GOBY_SMOKE_MEDIA_FILE"]).resolve(strict=True)

if cancel_root.parent != fixture_root or cancel_root.name != "cancel":
    raise RuntimeError("Cancellation fixtures must stay in /opt/goby-fixtures/cancel.")
if not source.is_relative_to(fixture_root) or not source.is_file():
    raise RuntimeError("The source must be a regular file inside /opt/goby-fixtures.")

cancel_root.mkdir(mode=0o755, exist_ok=True)
for index in range(200):
    target = cancel_root / f"cancel-{index:04d}.mp4"
    if target.is_symlink():
        raise RuntimeError(f"A symbolic link is not a cancellation fixture: {target.name}")
    if target.exists():
        if not target.samefile(source):
            raise RuntimeError(f"An unrelated fixture already exists: {target.name}")
    else:
        target.hardlink_to(source)

print("Prepared 200 media hard links in /opt/goby-fixtures/cancel; source media was preserved.")
