# Security boundaries

These are local maintenance tools, not a sandbox. Run them as your normal user.
Build commands retain your permissions: use a VM or another OS-enforced sandbox
with limited mounts, credentials, network and disk if you don't trust the code.
The 5 GiB reserve is a polling target, not a quota on unrelated processes.

- No runtime third-party packages, telemetry, self-update, sudo or network client.
  Installation of command shims and a background job is opt-in.
- Repository paths are passed as arguments, never evaluated by a shell. Audit Git
  calls disable hooks and filesystem monitors; they don't fetch or run builds.
- Worktree removal uses Git without force, requires explicit selection and an
  inactivity confirmation, and refuses unknown, primary, locked, dirty, unmerged
  or open worktrees. Untracked and ignored files count as data to preserve.
- Cache cleanup is limited to managed directories. Symlinked or other-user/
  group-writable managed directories are rejected. State/config lives outside repos.
- The guard terminates only the process group it creates. It never selects host
  processes by name. Existing builds are not retroactively managed.
- No protection is claimed against a malicious process already running as the
  same user, forged environment variables, detached processes, or changes racing
  an explicit inactivity confirmation. Git and `lsof` are trusted executables.

Download releases from this repository and verify SHA-256 checksums. Checksums
detect corruption, not publisher compromise. Pin a reviewed source commit when
building. Binaries are currently unsigned; do not mistake checksums for signatures.
Tests exercise refusal paths, process termination, locks, hostile path names,
symlinks and repository-configured executables.
