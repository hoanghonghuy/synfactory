#!/usr/bin/env bash
set -euo pipefail

ROOT="${1:-upstreams}"
mkdir -p "$ROOT"
cd "$ROOT"

repos=(
  "https://github.com/miniforge-ai/miniforge.git"
  "https://github.com/SebaBoler/vanguard.git"
  "https://github.com/racecraft-lab/Paddock.git"
  "https://github.com/vercel-labs/eve-software-factory-template.git"
  "https://github.com/disler/super-simple-software-factory.git"
  "https://github.com/All-Hands-AI/OpenHands.git"
  "https://github.com/OpenHands/extensions.git"
  "https://github.com/FoundationAgents/MetaGPT.git"
  "https://github.com/OpenBMB/ChatDev.git"
)

for repo in "${repos[@]}"; do
  name="$(basename "$repo" .git)"
  owner="$(basename "$(dirname "$repo")")"
  dir="${owner}__${name}"

  if [[ -d "$dir/.git" ]]; then
    echo "[update] $dir"
    git -C "$dir" fetch --depth=1 origin
    default_branch="$(git -C "$dir" remote show origin | sed -n '/HEAD branch/s/.*: //p')"
    if [[ -n "$default_branch" ]]; then
      git -C "$dir" reset --hard "origin/$default_branch"
    fi
    continue
  fi

  echo "[clone] $repo -> $dir"
  git clone --depth=1 --filter=blob:none "$repo" "$dir"
done

echo
echo "Upstreams ready in: $(pwd)"
