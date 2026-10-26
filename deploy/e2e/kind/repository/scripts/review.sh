set -eu

marker=$(cat scripts/review.txt)
printf 'acceptance reviewed Git run: %s\n' "$marker"
