"""Prepare dependency downloads outside semantic test evidence collection."""

import subprocess


def prepare(directory, env):
    """Fail setup explicitly, keeping test stderr rejection unconditional."""
    try:
        subprocess.run(
            ["go", "mod", "download", "all"],
            cwd=directory,
            env=env,
            check=True,
            timeout=300,
        )
    except (subprocess.CalledProcessError, subprocess.TimeoutExpired) as error:
        raise SystemExit(
            "Dependency preparation failed; no SQL evidence"
        ) from error
