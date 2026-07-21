# Git Workflow

Date: 2026-07-21

Use `main` as the stable branch. New work should happen on short-lived feature branches and be merged back after tests pass.

## Branches

Branch naming:

- `feature/<name>` for product functionality;
- `chore/<name>` for tooling, repository, and infrastructure hygiene;
- `docs/<name>` for documentation-only work;
- `fix/<name>` for bug fixes.

Default flow:

```sh
git switch main
git switch -c feature/video-sampling
# edit code
go test ./...
git add -A
scripts/check_git_index.sh
git commit -m "feat: add video sampling command"
git switch main
git merge --no-ff feature/video-sampling
```

## Local Data Policy

The repository tracks source code, scripts, docs, manifests, migrations, and small fixtures.

The repository does not track:

- raw VODs;
- YouTube metadata dumps;
- generated frames, clips, probe outputs, contact sheets, and reports;
- local build outputs;
- local Go cache;
- environment files;
- large media files.

These paths are ignored by `.gitignore`. Before committing, run:

```sh
scripts/check_git_index.sh
```

The check fails if staged files include local data paths, media extensions, or files larger than `50 MiB`.

Override the size threshold only for intentional small binary fixtures:

```sh
MAX_GIT_FILE_BYTES=10485760 scripts/check_git_index.sh
```

## Commit Rules

Use concise conventional-style subjects:

```text
feat: add video probe command
fix: validate duplicate VOD labels
docs: document Kafka event stream
chore: configure git safety checks
```

Keep commits small enough to explain. Include generated artifacts only when they are deliberately tiny test fixtures.

