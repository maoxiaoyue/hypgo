#!/bin/sh
# HypGo AI Diff Logger — 手動觸發
# 用法：./scripts/ai-diff-log.sh
# 記錄當前所有未 commit 的改動到 logs/ai.diff_<YYYYMMDD>.log

LOGDIR="logs"
mkdir -p "$LOGDIR"

DATE=$(date +%Y%m%d)
LOGFILE="$LOGDIR/ai.diff_${DATE}.log"
TIMESTAMP=$(date "+%Y-%m-%d %H:%M:%S")

# 取得所有改動（staged + unstaged）
DIFF=$(git diff --stat HEAD 2>/dev/null)
DIFF_DETAIL=$(git diff --numstat HEAD 2>/dev/null)

# 加上 untracked files
UNTRACKED=$(git ls-files --others --exclude-standard 2>/dev/null)

if [ -z "$DIFF" ] && [ -z "$UNTRACKED" ]; then
    echo "No changes to log."
    exit 0
fi

{
    echo "===== $TIMESTAMP ====="
    echo ""
    echo "Branch: $(git branch --show-current 2>/dev/null || echo 'unknown')"
    echo ""
    echo "--- Changed Files ---"
    if [ -n "$DIFF" ]; then
        echo "$DIFF"
    fi
    echo ""
    echo "--- Line Changes (added/deleted/file) ---"
    if [ -n "$DIFF_DETAIL" ]; then
        echo "$DIFF_DETAIL"
    fi
    if [ -n "$UNTRACKED" ]; then
        echo ""
        echo "--- New Files (untracked) ---"
        echo "$UNTRACKED" | while read f; do
            lines=$(wc -l < "$f" 2>/dev/null || echo "?")
            echo "  + $f ($lines lines)"
        done
    fi
    echo ""
    echo "=========================================="
    echo ""
} >> "$LOGFILE"

echo "Diff logged to $LOGFILE"
