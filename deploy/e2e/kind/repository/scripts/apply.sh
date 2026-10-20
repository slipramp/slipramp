set -eu

plan=$(cat /slipramp/inputs/plan.txt)
test "$plan" = "plan:hello-kind"
printf 'acceptance apply consumed: %s\n' "$plan"
