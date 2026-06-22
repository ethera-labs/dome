#!/usr/bin/env bash
# Run every Test* in test/ against one of the remote networks, file by file,
# in a deliberate order. Captures full output to <env>.log and prints a
# grouped pass/fail summary table at the end.
#
# Usage: scripts/full-test-suite.sh [--no-stress] <env>
#   env: prod-sepolia | prod-hoodi | stage-sepolia
#
# Flags:
#   --no-stress   Skip the entire Stress group. Note: L2→L1 phase 2
#                 (finalize) tests will likely SKIP without the stress
#                 group's wait time, since the dispute game won't be mature.
#
# Ordering rationale:
#   1. L1 → L2                  (EOA + SA, "normal" first)
#   2. L2 ↔ L2                  (both directions, plus XT edge cases)
#   3. L2 → L1 phase 1          (just trigger the withdrawal on L2)
#   4. Stress                   (also fills the dispute-game maturation window)
#   5. L2 → L1 phase 2          (finalize / replay on L1 — should be mature now)
#
# Anything new under test/ that isn't listed in ENTRIES below is picked up
# automatically as a final "Uncategorized" group so new tests can't silently
# fall off the suite.
#
# A file is FAIL if at least one of its tests printed `--- FAIL:`, PASS if at
# least one printed `--- PASS:` and none failed, otherwise SKIP. Each row is
# invoked in its own `bin/dome -test.run=...` call so in-file ordering (e.g.
# _DeployPhase before _BridgePhase) is preserved.

set -uo pipefail

ENV=""
NO_STRESS=0
while (( $# > 0 )); do
  case "$1" in
    --no-stress) NO_STRESS=1; shift ;;
    -h|--help)
      sed -n '2,12p' "$0" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    --*) echo "unknown flag: $1" >&2; exit 2 ;;
    *)
      if [[ -n "$ENV" ]]; then
        echo "unexpected extra arg: $1" >&2; exit 2
      fi
      ENV="$1"; shift
      ;;
  esac
done

if [[ -z "$ENV" ]]; then
  echo "usage: $0 [--no-stress] <env: prod-sepolia|prod-hoodi|stage-sepolia>" >&2
  exit 2
fi

case "$ENV" in
  prod-sepolia)   CONFIG="configs/config.sepolia-prod.yaml" ;;
  prod-hoodi)     CONFIG="configs/config.hoodi.yaml" ;;
  stage-sepolia)  CONFIG="configs/config.sepolia-stage.yaml" ;;
  *)
    echo "unknown env: $ENV (expected prod-sepolia|prod-hoodi|stage-sepolia)" >&2
    exit 2
    ;;
esac

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$REPO_ROOT"

if [[ ! -f "$CONFIG" ]]; then
  echo "config not found: $CONFIG" >&2
  exit 1
fi

LOG_FILE="${ENV}.log"
BIN="${DOME_BIN:-bin/dome}"
TIMEOUT="${TEST_TIMEOUT:-2h}"

: > "$LOG_FILE"

emit() { printf '%s\n' "$*" | tee -a "$LOG_FILE"; }

if [[ ! -x "$BIN" ]]; then
  emit "Building $BIN ..."
  if ! make build >> "$LOG_FILE" 2>&1; then
    emit "build failed; see $LOG_FILE"
    exit 1
  fi
fi

# ENTRIES: ordered list of directives. Each line is one of:
#   GROUP|<heading>
#   RUN|<file_stem>[|<tag>[|<name_filter_regex>]]
#
# RUN entries:
#   - <file_stem> is the test file without ".go".
#   - <tag> (optional) annotates the row when the same file runs in multiple
#     phases (e.g. L2→L1 [Withdraw] vs [Finalize]).
#   - <name_filter_regex> (optional) filters which Test* in that file run.
#     The regex is matched against test function names (anywhere); the chosen
#     names are joined into `-test.run='^(T1|T2|...)$'`.
ENTRIES=(
  "GROUP|L1 → L2  (normal, EOA + SA)"
  "RUN|l1_to_l2_eth_test"
  "RUN|l1_to_l2_new_token_test"
  "RUN|l1_to_l2_existing_token_test"
  "RUN|l1_to_l2_sa_eth_test"
  "RUN|l1_to_l2_sa_new_token_test"

  "GROUP|L2 ↔ L2  (both directions, including XT edge cases)"
  "RUN|l2_to_l2_eth_test"
  "RUN|l2_to_l2_new_token_test"
  "RUN|l2_to_l2_existing_token_test"
  "RUN|l2_to_l2_sa_eth_test"
  "RUN|l2_to_l2_sa_new_token_test"
  "RUN|l2_to_l2_sa_existing_token_test"
  "RUN|l2_to_l2_manual_test"
  "RUN|l2_redeem_wrapped_cet_test"
  "RUN|replay_attack_test"
  "RUN|bridge_invalid_args_test"
  "RUN|bridge_receiver_callback_test"
  "RUN|xt_state_drift_test"
  "RUN|xt_nonce_race_test"
  "RUN|xt_put_inbox_nonce_test"

  "GROUP|L2 → L1  phase 1 — trigger withdraw on L2"
  "RUN|l2_to_l1_eth_test|Withdraw|.*_Withdraw_.*"
  "RUN|l2_to_l1_token_test|Withdraw|.*_Withdraw_.*"
  "RUN|l2_to_l1_sa_eth_test|Withdraw|.*_Withdraw_.*"
  "RUN|l2_to_l1_sa_token_test|Withdraw|.*_Withdraw_.*"
  "RUN|l2_to_l1_replay_test|Withdraw|.*_Withdraw_.*"

  "GROUP|Stress  (also fills the dispute-game maturation window)"
  "RUN|l1_to_l2_eth_stress_test"
  "RUN|l1_to_l2_new_token_stress_test"
  "RUN|l2_to_l2_sa_existing_token_stress_test"

  "GROUP|L2 → L1  phase 2 — finalize / replay on L1"
  "RUN|l2_to_l1_eth_test|Finalize|.*_Finalize_.*"
  "RUN|l2_to_l1_token_test|Finalize|.*_Finalize_.*"
  "RUN|l2_to_l1_sa_eth_test|Finalize|.*_Finalize_.*"
  "RUN|l2_to_l1_sa_token_test|Finalize|.*_Finalize_.*"
  "RUN|l2_to_l1_replay_test|Replay|.*_Replay_.*"
)

# Auto-append any *_test.go file that isn't already covered by the static
# list. Prevents new tests from silently falling off the suite.
COVERED=" "
for e in "${ENTRIES[@]}"; do
  if [[ "$e" == "RUN|"* ]]; then
    rest="${e#RUN|}"
    f="${rest%%|*}"
    COVERED="$COVERED$f "
  fi
done
UNCATEGORIZED=()
while IFS= read -r fname; do
  stem="${fname%.go}"
  if [[ "$COVERED" != *" $stem "* ]]; then
    UNCATEGORIZED+=("$stem")
  fi
done < <(
  find test -maxdepth 1 -type f -name '*_test.go' ! -name 'main_test.go' \
    -exec basename {} \; | sort
)
if (( ${#UNCATEGORIZED[@]} > 0 )); then
  ENTRIES+=("GROUP|Uncategorized  (not in static list — appended automatically)")
  for s in "${UNCATEGORIZED[@]}"; do
    ENTRIES+=("RUN|$s")
  done
fi

# Apply --no-stress: drop the "Stress" GROUP and every RUN entry under it
# (up to the next GROUP).
if (( NO_STRESS == 1 )); then
  FILTERED=()
  IN_STRESS=0
  for e in "${ENTRIES[@]}"; do
    case "$e" in
      "GROUP|Stress"*) IN_STRESS=1; continue ;;
      "GROUP|"*)       IN_STRESS=0 ;;
    esac
    (( IN_STRESS == 0 )) && FILTERED+=("$e")
  done
  ENTRIES=("${FILTERED[@]}")
fi

# Count total RUN entries up front for [IDX/TOTAL] progress display.
TOTAL=0
for e in "${ENTRIES[@]}"; do
  [[ "$e" == "RUN|"* ]] && TOTAL=$((TOTAL+1))
done

START_TS=$(date '+%Y-%m-%d %H:%M:%S')
emit "===================================================================="
emit " full-test-suite"
emit "   env:       $ENV"
emit "   config:    $CONFIG"
emit "   log:       $LOG_FILE"
emit "   timeout:   $TIMEOUT  (override via TEST_TIMEOUT=...)"
emit "   total:     $TOTAL test-file runs (across grouped phases)"
(( NO_STRESS == 1 )) && emit "   stress:    disabled (--no-stress)"
emit "   started:   $START_TS"
emit "===================================================================="

# SUMMARY rows. Each row is "TYPE|payload":
#   GROUP|<heading>          → section divider in the table
#   ROW|<label>|<result>|<duration>
SUMMARY=()
PASS=0; FAIL=0; SKIP=0; IDX=0
SUITE_START=$(date +%s)
CURRENT_GROUP=""

for e in "${ENTRIES[@]}"; do
  case "$e" in
    "GROUP|"*)
      CURRENT_GROUP="${e#GROUP|}"
      SUMMARY+=("GROUP|$CURRENT_GROUP")
      emit ""
      emit "==================================================================="
      emit "  $CURRENT_GROUP"
      emit "==================================================================="
      {
        echo ""
        echo "##### $CURRENT_GROUP #####"
      } >> "$LOG_FILE"
      ;;
    "RUN|"*)
      IDX=$((IDX+1))
      rest="${e#RUN|}"
      FILE_STEM=""; TAG=""; PATTERN_OVERRIDE=""
      IFS='|' read -r FILE_STEM TAG PATTERN_OVERRIDE <<< "$rest"
      PATH_FILE="test/${FILE_STEM}.go"
      LABEL="$FILE_STEM"
      [[ -n "$TAG" ]] && LABEL="${FILE_STEM} [${TAG}]"

      if [[ ! -f "$PATH_FILE" ]]; then
        SUMMARY+=("ROW|$LABEL|MISS|0")
        SKIP=$((SKIP+1))
        emit ""
        emit "[$IDX/$TOTAL] $LABEL  → MISS (file $PATH_FILE not found)"
        continue
      fi

      ALL=$(grep -oE '^func Test[A-Za-z0-9_]+' "$PATH_FILE" | sed 's/^func //' | sort -u)
      if [[ -n "$PATTERN_OVERRIDE" ]]; then
        FILTERED=$(echo "$ALL" | grep -E "$PATTERN_OVERRIDE" || true)
      else
        FILTERED="$ALL"
      fi

      if [[ -z "$FILTERED" ]]; then
        SUMMARY+=("ROW|$LABEL|SKIP|0")
        SKIP=$((SKIP+1))
        emit ""
        emit "[$IDX/$TOTAL] $LABEL  → SKIP (no Test* matched)"
        continue
      fi

      JOINED=$(echo "$FILTERED" | paste -sd'|' -)
      PATTERN="^(${JOINED})$"

      emit ""
      emit "[$IDX/$TOTAL] $LABEL  (started $(date '+%H:%M:%S'))"
      {
        echo ""
        echo "===== [$IDX/$TOTAL] $LABEL ====="
        echo "pattern: $PATTERN"
      } >> "$LOG_FILE"

      TMP=$(mktemp)
      T0=$(date +%s)
      CONFIG_PATH="$REPO_ROOT/$CONFIG" LOG_LEVEL=INFO \
        "$BIN" -test.v -test.count=1 -test.timeout="$TIMEOUT" -test.run="$PATTERN" \
        > "$TMP" 2>&1
      EXIT=$?
      T1=$(date +%s)
      DUR=$((T1 - T0))

      cat "$TMP" >> "$LOG_FILE"
      HAS_FAIL=$(grep -c '^--- FAIL:' "$TMP" || true)
      HAS_PASS=$(grep -c '^--- PASS:' "$TMP" || true)
      rm -f "$TMP"

      if (( HAS_FAIL > 0 )); then
        RESULT=FAIL; FAIL=$((FAIL+1))
      elif (( HAS_PASS > 0 )); then
        RESULT=PASS; PASS=$((PASS+1))
      elif (( EXIT != 0 )); then
        # build error / panic before first test logged
        RESULT=FAIL; FAIL=$((FAIL+1))
      else
        RESULT=SKIP; SKIP=$((SKIP+1))
      fi
      SUMMARY+=("ROW|$LABEL|$RESULT|$DUR")
      emit "[$IDX/$TOTAL] $LABEL  → $RESULT  (${DUR}s, exit=$EXIT)"
      ;;
  esac
done

SUITE_END=$(date +%s)
SUITE_DUR=$((SUITE_END - SUITE_START))
END_TS=$(date '+%Y-%m-%d %H:%M:%S')

# Compute column widths from row labels + group headings.
MAXW=9  # "TEST FILE"
for r in "${SUMMARY[@]}"; do
  case "$r" in
    "ROW|"*)
      payload="${r#ROW|}"
      lbl="${payload%%|*}"
      (( ${#lbl} > MAXW )) && MAXW=${#lbl}
      ;;
    "GROUP|"*)
      h="${r#GROUP|}"
      (( ${#h} > MAXW )) && MAXW=${#h}
      ;;
  esac
done

DASH_FILE=$(printf '%*s' "$((MAXW+2))" '' | tr ' ' '-')
DASH_RES='--------'
DASH_DUR='-------'
ROW_SEP="+${DASH_FILE}+${DASH_RES}+${DASH_DUR}+"
# Group separator: single wide cell spanning everything.
GROUP_WIDTH=$((MAXW + 2 + 8 + 7 + 2))  # file-col + res-col + dur-col + 2 inner pipes
DASH_GROUP=$(printf '%*s' "$GROUP_WIDTH" '' | tr ' ' '=')
GROUP_SEP="+${DASH_GROUP}+"

emit ""
emit "===================================================================="
emit " SUMMARY  ($ENV)"
emit "   started:   $START_TS"
emit "   finished:  $END_TS"
emit "   elapsed:   ${SUITE_DUR}s"
emit "   log:       $LOG_FILE"
emit "===================================================================="

FIRST=1
for r in "${SUMMARY[@]}"; do
  case "$r" in
    "GROUP|"*)
      h="${r#GROUP|}"
      [[ $FIRST -eq 0 ]] && emit "$ROW_SEP"
      emit "$GROUP_SEP"
      # Center the heading inside the wide cell.
      pad=$(( (GROUP_WIDTH - ${#h}) / 2 ))
      (( pad < 0 )) && pad=0
      printf -v cell '%*s%s%*s' "$pad" '' "$h" "$((GROUP_WIDTH - pad - ${#h}))" ''
      emit "|${cell}|"
      emit "$GROUP_SEP"
      emit "$(printf '| %-*s | %-6s | %-5s |' "$MAXW" "TEST FILE" "RESULT" "TIME")"
      emit "$ROW_SEP"
      FIRST=0
      ;;
    "ROW|"*)
      payload="${r#ROW|}"
      lbl="${payload%%|*}"; rest="${payload#*|}"
      res="${rest%|*}"; dur="${rest##*|}"
      emit "$(printf '| %-*s | %-6s | %4ss |' "$MAXW" "$lbl" "$res" "$dur")"
      ;;
  esac
done
emit "$ROW_SEP"
emit ""
emit "Total: $((PASS+FAIL+SKIP)) | Passed: $PASS | Failed: $FAIL | Skipped: $SKIP"
emit ""

if (( FAIL > 0 )); then
  exit 1
fi
