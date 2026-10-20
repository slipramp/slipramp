set -eu

printf 'plan:%s\n' "$ACCEPTANCE_MESSAGE" > /slipramp/artifacts/plan.txt
printf 'acceptance preview complete: %s\n' "$ACCEPTANCE_MESSAGE"
