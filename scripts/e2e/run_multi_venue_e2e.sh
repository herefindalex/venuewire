#!/usr/bin/env bash
set -euo pipefail

require_gate() {
  local name=$1
  if [[ ${!name:-0} != 1 ]]; then
    echo "required gate is not 1: $name" >&2
    exit 2
  fi
}

for gate in RUN_MULTI_VENUE_E2E RUN_BYBIT_READ_TESTS RUN_BYBIT_TRADING_TESTS RUN_DERIBIT_READ_TESTS RUN_DERIBIT_TRADING_TESTS; do
  require_gate "$gate"
done

bybit_fix_status=BLOCKED_GATE
deribit_fix_status=BLOCKED_GATE

if [[ -n ${VENUEWIRE_BINARY:-} ]]; then
  binary=$VENUEWIRE_BINARY
elif [[ -n ${BYBITCTL_BINARY:-} ]]; then
  echo "warning: BYBITCTL_BINARY is deprecated; use VENUEWIRE_BINARY" >&2
  binary=$BYBITCTL_BINARY
else
  binary=./bin/venuewire
fi
if [[ ! -x $binary ]]; then
  echo "build executable first: go build -o ./bin/venuewire ./cmd/venuewire" >&2
  exit 2
fi

e2e_tmp=$(mktemp -d)
export BYBIT_STATE_FILE="$e2e_tmp/orders.json"
export MULTI_VENUE_INTENT_FILE="$e2e_tmp/intents.json"
bybit_cleanup_needed=0
deribit_cleanup_needed=0
bybit_qty=
deribit_amount=
bybit_open_link=
deribit_open_order_id=

cleanup() {
  set +e
  if [[ -n ${bybit_open_link:-} ]]; then
    "$binary" order cancel --category linear --symbol ETHUSDT --order-link-id "$bybit_open_link" >/dev/null 2>&1
  fi
  if [[ -n ${deribit_open_order_id:-} ]]; then
    "$binary" --venue deribit order cancel --order-id "$deribit_open_order_id" --confirm >/dev/null 2>&1
  fi
  if [[ $bybit_cleanup_needed == 1 && -n ${bybit_qty:-} ]]; then
    "$binary" order place --category linear --symbol ETHUSDT --side Sell --type Market --qty "$bybit_qty" --reduce-only >/dev/null 2>&1
  fi
  if [[ $deribit_cleanup_needed == 1 && -n ${deribit_amount:-} ]]; then
    local cleanup_plan cleanup_id
    cleanup_plan=$("$binary" --venue deribit order plan --instrument BTC-PERPETUAL --side sell --amount "$deribit_amount" --type market --reduce-only 2>/dev/null)
    cleanup_id=$(jq -r .id <<<"$cleanup_plan")
    "$binary" --venue deribit order execute --plan-id "$cleanup_id" --confirm >/dev/null 2>&1
  fi
  [[ -n ${bybit_private_pid:-} ]] && kill "$bybit_private_pid" 2>/dev/null
  [[ -n ${deribit_private_pid:-} ]] && kill "$deribit_private_pid" 2>/dev/null
  if [[ ${KEEP_E2E_TMP:-0} == 1 ]]; then
    echo "retained E2E artifacts: $e2e_tmp" >&2
  else
    rm -r -- "$e2e_tmp"
  fi
}
trap cleanup EXIT
trap 'echo "E2E assertion failed at line $LINENO" >&2' ERR

echo "E2E: shared read surfaces"
"$binary" help >"$e2e_tmp/help.txt"

echo "E2E: reversible local state migration"
operational_state_file=$BYBIT_STATE_FILE
migration_state_file="$e2e_tmp/migration/orders.json"
mkdir -p "$(dirname "$migration_state_file")"
printf '%s\n' '{"version":1,"updatedAt":"2026-09-09T00:00:00Z","orders":{"fixture":{"category":"linear","orderId":"migration-order","orderLinkId":"migration-link"}},"executions":{}}' >"$migration_state_file"
chmod 600 "$migration_state_file"
export BYBIT_STATE_FILE=$migration_state_file
"$binary" state migrate-v1 --bybit-account-alias bybit-test --dry-run >"$e2e_tmp/migration-dry-run.json"
jq -e '.FromVersion == 1 and .ToVersion == 2 and .DryRun == true and .Changed == true' "$e2e_tmp/migration-dry-run.json" >/dev/null
jq -e '.version == 1' "$migration_state_file" >/dev/null
"$binary" state migrate-v1 --bybit-account-alias bybit-test >"$e2e_tmp/migration-apply.json"
jq -e '.FromVersion == 1 and .ToVersion == 2 and .DryRun == false and .Changed == true' "$e2e_tmp/migration-apply.json" >/dev/null
jq -e '.version == 2' "$migration_state_file" >/dev/null
[[ -f $migration_state_file.v1.bak ]]
"$binary" state restore-v1 >"$e2e_tmp/migration-restore.json"
jq -e '.version == 1' "$migration_state_file" >/dev/null
export BYBIT_STATE_FILE=$operational_state_file

"$binary" --venue all status >"$e2e_tmp/all-status.json"
jq -e '.allHealthy == true and ([.results[].ok] | all)' "$e2e_tmp/all-status.json" >/dev/null
"$binary" --venue all portfolio >"$e2e_tmp/portfolio.json"
jq -e '.allHealthy == true and (.results | length == 2)' "$e2e_tmp/portfolio.json" >/dev/null

echo "E2E: local and gated live FIX commands"
"$binary" --venue bybit fix mock-demo >"$e2e_tmp/bybit-fix-mock.json"
jq -e '(.orders | length) > 0' "$e2e_tmp/bybit-fix-mock.json" >/dev/null
if timeout 1s "$binary" --venue bybit fix mock-server --listen 127.0.0.1:19001 >"$e2e_tmp/bybit-fix-server.log" 2>&1; then
  fix_server_code=0
else
  fix_server_code=$?
fi
[[ $fix_server_code == 124 ]]
"$binary" --venue deribit fix mock-demo >"$e2e_tmp/deribit-fix-mock.json"
jq -e '.validationLevel == "LOCAL_TESTED" and .mockFinalOrderStatus == "4" and (.tradingWritePerformed == false)' "$e2e_tmp/deribit-fix-mock.json" >/dev/null

if [[ ${RUN_BYBIT_FIX_TESTS:-0} == 1 ]]; then
  "$binary" --venue bybit fix connect-testnet >"$e2e_tmp/bybit-fix-live.json"
  bybit_fix_status=PASS
else
  echo "E2E: Bybit live FIX BLOCKED_GATE (RUN_BYBIT_FIX_TESTS != 1)" >&2
fi

if [[ ${RUN_DERIBIT_FIX_TESTS:-0} == 1 ]]; then
  "$binary" --venue deribit fix connect-testnet --duration 3s >"$e2e_tmp/deribit-fix-logon.json"
  jq -e '.validationLevel == "TESTNET_LOGON" and .authenticated and (.tradingWritePerformed == false)' "$e2e_tmp/deribit-fix-logon.json" >/dev/null
else
  echo "E2E: Deribit live FIX BLOCKED_GATE (RUN_DERIBIT_FIX_TESTS != 1)" >&2
fi

echo "E2E: Bybit reads and timed streams"
"$binary" --venue bybit time >"$e2e_tmp/bybit-time.json"
"$binary" --venue bybit instrument --category linear --symbol ETHUSDT >"$e2e_tmp/bybit-instrument.json"
"$binary" --venue bybit ticker --category linear --symbol ETHUSDT >"$e2e_tmp/bybit-ticker.json"
"$binary" --venue bybit account info >"$e2e_tmp/bybit-account.json"
"$binary" --venue bybit account balances >"$e2e_tmp/bybit-balances.json"
"$binary" --venue bybit positions --category linear --symbol ETHUSDT >"$e2e_tmp/bybit-position-before.json"
jq -e 'all(.[]; (.size | tonumber) == 0)' "$e2e_tmp/bybit-position-before.json" >/dev/null
bybit_qty=$(jq -er '.[0].lotSizeFilter.minOrderQty' "$e2e_tmp/bybit-instrument.json")
bybit_tick=$(jq -er '.[0].priceFilter.tickSize' "$e2e_tmp/bybit-instrument.json")
bybit_bid=$(jq -er '.[0].bid1Price' "$e2e_tmp/bybit-ticker.json")
bybit_passive_price=$(awk -v p="$bybit_bid" -v t="$bybit_tick" 'BEGIN { printf "%.8f", int((p*0.99)/t)*t }')
bybit_amend_price=$(awk -v p="$bybit_bid" -v t="$bybit_tick" 'BEGIN { printf "%.8f", int((p*0.995)/t)*t }')

if timeout 5s "$binary" --venue bybit market trades --symbol ETHUSDT >"$e2e_tmp/bybit-trades-stream.jsonl" 2>&1; then
  stream_code=0
else
  stream_code=$?
fi
[[ $stream_code == 0 || $stream_code == 124 ]] && [[ -s $e2e_tmp/bybit-trades-stream.jsonl ]]
if timeout 5s "$binary" --venue bybit market orderbook --symbol ETHUSDT --depth 50 >"$e2e_tmp/bybit-book-stream.jsonl" 2>&1; then
  stream_code=0
else
  stream_code=$?
fi
[[ $stream_code == 0 || $stream_code == 124 ]] && [[ -s $e2e_tmp/bybit-book-stream.jsonl ]]
timeout 35s "$binary" --venue bybit private-stream --symbol ETHUSDT >"$e2e_tmp/bybit-private.jsonl" 2>&1 &
bybit_private_pid=$!
sleep 2

echo "E2E: Bybit independently verified passive lifecycle"
bybit_passive_link="e2e-bp-$(date +%s%N | tail -c 17)"
bybit_open_link=$bybit_passive_link
"$binary" --venue bybit order place --category linear --symbol ETHUSDT --side Buy --type Limit --qty "$bybit_qty" --price "$bybit_passive_price" --order-link-id "$bybit_passive_link" >"$e2e_tmp/bybit-place.json"
"$binary" --venue bybit order status --category linear --symbol ETHUSDT --order-link-id "$bybit_passive_link" >"$e2e_tmp/bybit-status-open.json"
jq -e --arg link "$bybit_passive_link" 'any(.[]; .orderLinkId == $link and (.orderStatus == "New" or .orderStatus == "PartiallyFilled"))' "$e2e_tmp/bybit-status-open.json" >/dev/null
"$binary" --venue bybit order amend --category linear --symbol ETHUSDT --order-link-id "$bybit_passive_link" --qty "$bybit_qty" --price "$bybit_amend_price" >"$e2e_tmp/bybit-amend.json"
"$binary" --venue bybit order status --category linear --symbol ETHUSDT --order-link-id "$bybit_passive_link" >"$e2e_tmp/bybit-status-amended.json"
jq -e --arg price "$bybit_amend_price" 'any(.[]; ((.price|tonumber) == ($price|tonumber)))' "$e2e_tmp/bybit-status-amended.json" >/dev/null
"$binary" --venue bybit order cancel --category linear --symbol ETHUSDT --order-link-id "$bybit_passive_link" >"$e2e_tmp/bybit-cancel.json"
"$binary" --venue bybit order status --category linear --symbol ETHUSDT --order-link-id "$bybit_passive_link" >"$e2e_tmp/bybit-status-cancelled.json"
jq -e 'any(.[]; .orderStatus == "Cancelled")' "$e2e_tmp/bybit-status-cancelled.json" >/dev/null
bybit_open_link=

echo "E2E: Bybit minimum fill, canonical execution fee, reduce-only cleanup"
bybit_fill_link="e2e-bf-$(date +%s%N | tail -c 17)"
bybit_cleanup_link="e2e-bc-$(date +%s%N | tail -c 17)"
"$binary" --venue bybit order place --category linear --symbol ETHUSDT --side Buy --type Market --qty "$bybit_qty" --order-link-id "$bybit_fill_link" >"$e2e_tmp/bybit-fill-ack.json"
bybit_cleanup_needed=1
for _ in {1..20}; do
  "$binary" --venue bybit order status --category linear --symbol ETHUSDT --order-link-id "$bybit_fill_link" >"$e2e_tmp/bybit-fill-state.json"
  jq -e 'any(.[]; .orderStatus == "Filled")' "$e2e_tmp/bybit-fill-state.json" >/dev/null && break
  sleep 0.25
done
jq -e 'any(.[]; .orderStatus == "Filled")' "$e2e_tmp/bybit-fill-state.json" >/dev/null
"$binary" --venue bybit executions --category linear --symbol ETHUSDT --order-link-id "$bybit_fill_link" >"$e2e_tmp/bybit-fill-trades.json"
jq -e --arg qty "$bybit_qty" 'length > 0 and ((map(.execQty|tonumber)|add) == ($qty|tonumber)) and all(.[]; .execFee != "")' "$e2e_tmp/bybit-fill-trades.json" >/dev/null
"$binary" --venue bybit order place --category linear --symbol ETHUSDT --side Sell --type Market --qty "$bybit_qty" --order-link-id "$bybit_cleanup_link" --reduce-only >"$e2e_tmp/bybit-cleanup-ack.json"
bybit_cleanup_needed=0
for _ in {1..20}; do
  "$binary" --venue bybit positions --category linear --symbol ETHUSDT >"$e2e_tmp/bybit-position-after.json"
  jq -e 'all(.[]; (.size | tonumber) == 0)' "$e2e_tmp/bybit-position-after.json" >/dev/null && break
  sleep 0.25
done
jq -e 'all(.[]; (.size | tonumber) == 0)' "$e2e_tmp/bybit-position-after.json" >/dev/null
"$binary" --venue bybit executions --category linear --symbol ETHUSDT --order-link-id "$bybit_cleanup_link" >"$e2e_tmp/bybit-cleanup-trades.json"
jq -e 'length > 0 and all(.[]; .execFee != "")' "$e2e_tmp/bybit-cleanup-trades.json" >/dev/null
"$binary" --venue bybit reconcile --category linear --symbol ETHUSDT >"$e2e_tmp/bybit-reconcile.json"
rg -q "$bybit_fill_link" "$e2e_tmp/bybit-private.jsonl"

echo "E2E: Deribit reads and timed streams"
"$binary" --venue deribit doctor >"$e2e_tmp/deribit-doctor.json"
jq -e '.public.ok and .private.ok and (.tradingWritePerformed == false)' "$e2e_tmp/deribit-doctor.json" >/dev/null
"$binary" --venue deribit time >"$e2e_tmp/deribit-time.json"
"$binary" --venue deribit instruments --currency BTC --kind future >"$e2e_tmp/deribit-instruments.json"
"$binary" --venue deribit instrument --name BTC-PERPETUAL >"$e2e_tmp/deribit-instrument.json"
"$binary" --venue deribit ticker --instrument BTC-PERPETUAL >"$e2e_tmp/deribit-ticker.json"
"$binary" --venue deribit account balances --currency all >"$e2e_tmp/deribit-balances.json"
"$binary" --venue deribit positions --currency BTC --kind future >"$e2e_tmp/deribit-position-before.json"
jq -e 'all(.[]; (.size | tonumber) == 0)' "$e2e_tmp/deribit-position-before.json" >/dev/null
"$binary" --venue deribit orders list --instrument BTC-PERPETUAL >"$e2e_tmp/deribit-open-before.json"
jq -e 'length == 0' "$e2e_tmp/deribit-open-before.json" >/dev/null
"$binary" --venue deribit market trades --instrument BTC-PERPETUAL --duration 5s >"$e2e_tmp/deribit-market-trades.jsonl"
"$binary" --venue deribit market orderbook --instrument BTC-PERPETUAL --depth 10 --duration 5s >"$e2e_tmp/deribit-market-book.jsonl"
"$binary" --venue deribit public-stream --channels ticker.BTC-PERPETUAL.100ms --duration 5s >"$e2e_tmp/deribit-public-stream.jsonl"
"$binary" --venue deribit private-stream --duration 35s --enable-connection-cod --confirm >"$e2e_tmp/deribit-private.jsonl" &
deribit_private_pid=$!
sleep 2
deribit_amount=$(jq -er '.min_trade_amount' "$e2e_tmp/deribit-instrument.json")
deribit_tick=$(jq -er '.tick_size' "$e2e_tmp/deribit-instrument.json")
deribit_mark=$(jq -er '.mark_price' "$e2e_tmp/deribit-ticker.json")
deribit_price=$(awk -v p="$deribit_mark" -v t="$deribit_tick" 'BEGIN { printf "%.8f", int((p*0.99)/t)*t }')
deribit_amend=$(awk -v p="$deribit_mark" -v t="$deribit_tick" 'BEGIN { printf "%.8f", int((p*0.995)/t)*t }')

deribit_lifecycle() {
  local transport=$1 prefix=$2 plan plan_id execution order_id
  local mutation_transport=$transport
  plan=$("$binary" --venue deribit order plan --instrument BTC-PERPETUAL --side buy --amount "$deribit_amount" --type limit --price "$deribit_price" --transport "$transport" --post-only)
  jq -e --arg transport "$transport" '.status == "Planned" and .amountUnit == "USD_notional" and .transport == $transport' <<<"$plan" >/dev/null
  plan_id=$(jq -er .id <<<"$plan")
  execution=$("$binary" --venue deribit order execute --plan-id "$plan_id" --confirm)
  jq -e '.verified == true and .readState.order_state == "open"' <<<"$execution" >/dev/null
  order_id=$(jq -er .readState.order_id <<<"$execution")
  deribit_open_order_id=$order_id
  "$binary" --venue deribit order status --order-id "$order_id" >"$e2e_tmp/$prefix-status.json"
  jq -e --arg id "$order_id" '.order_id == $id and .order_state == "open"' "$e2e_tmp/$prefix-status.json" >/dev/null
  "$binary" --venue deribit order amend --order-id "$order_id" --amount "$deribit_amount" --price "$deribit_amend" --transport "$mutation_transport" --confirm >"$e2e_tmp/$prefix-amend.json"
  jq -e '.verified == true' "$e2e_tmp/$prefix-amend.json" >/dev/null
  "$binary" --venue deribit order cancel --order-id "$order_id" --transport "$mutation_transport" --confirm >"$e2e_tmp/$prefix-cancel.json"
  jq -e '.verified == true and .readState.order_state == "cancelled"' "$e2e_tmp/$prefix-cancel.json" >/dev/null
  deribit_open_order_id=
  "$binary" --venue deribit order trades --order-id "$order_id" >"$e2e_tmp/$prefix-trades.json"
  jq -e 'length == 0' "$e2e_tmp/$prefix-trades.json" >/dev/null
}

echo "E2E: Deribit HTTP and WS passive lifecycles"
deribit_lifecycle http deribit-http
deribit_lifecycle ws deribit-ws
if [[ ${RUN_DERIBIT_FIX_TESTS:-0} == 1 ]]; then
  echo "E2E: Deribit FIX passive lifecycle"
  deribit_lifecycle fix deribit-fix
  deribit_fix_status=PASS
fi

echo "E2E: Deribit minimum fill, canonical fee match, reduce-only cleanup"
deribit_fill_plan=$("$binary" --venue deribit order plan --instrument BTC-PERPETUAL --side buy --amount "$deribit_amount" --type market --transport http)
deribit_fill_id=$(jq -er .id <<<"$deribit_fill_plan")
deribit_fill=$("$binary" --venue deribit order execute --plan-id "$deribit_fill_id" --confirm)
jq -e '.verified == true and .readState.order_state == "filled"' <<<"$deribit_fill" >/dev/null
deribit_fill_order=$(jq -er .readState.order_id <<<"$deribit_fill")
deribit_cleanup_needed=1
"$binary" --venue deribit order trades --order-id "$deribit_fill_order" >"$e2e_tmp/deribit-fill-trades.json"
jq -e 'length > 0' "$e2e_tmp/deribit-fill-trades.json" >/dev/null
ack_amount=$(jq -r '[.acknowledgement.trades[].amount|tonumber]|add//0' <<<"$deribit_fill")
read_amount=$(jq -r '[.[].amount|tonumber]|add//0' "$e2e_tmp/deribit-fill-trades.json")
ack_fee=$(jq -r '[.acknowledgement.trades[].fee|tonumber]|add//0' <<<"$deribit_fill")
read_fee=$(jq -r '[.[].fee|tonumber]|add//0' "$e2e_tmp/deribit-fill-trades.json")
awk -v a="$ack_amount" -v b="$read_amount" -v c="$ack_fee" -v d="$read_fee" 'BEGIN { if (a != b || c != d) exit 1 }'
deribit_cleanup_plan=$("$binary" --venue deribit order plan --instrument BTC-PERPETUAL --side sell --amount "$deribit_amount" --type market --reduce-only --transport http)
deribit_cleanup_id=$(jq -er .id <<<"$deribit_cleanup_plan")
deribit_cleanup=$("$binary" --venue deribit order execute --plan-id "$deribit_cleanup_id" --confirm)
jq -e '.verified == true and .readState.order_state == "filled"' <<<"$deribit_cleanup" >/dev/null
deribit_cleanup_order=$(jq -er .readState.order_id <<<"$deribit_cleanup")
deribit_cleanup_needed=0
"$binary" --venue deribit order trades --order-id "$deribit_cleanup_order" >"$e2e_tmp/deribit-cleanup-trades.json"
jq -e 'length > 0' "$e2e_tmp/deribit-cleanup-trades.json" >/dev/null
for _ in {1..20}; do
  "$binary" --venue deribit positions --currency BTC --kind future >"$e2e_tmp/deribit-position-after.json"
  jq -e 'all(.[]; (.size | tonumber) == 0)' "$e2e_tmp/deribit-position-after.json" >/dev/null && break
  sleep 0.25
done
jq -e 'all(.[]; (.size | tonumber) == 0)' "$e2e_tmp/deribit-position-after.json" >/dev/null
"$binary" --venue deribit reconcile >"$e2e_tmp/deribit-reconcile.json"

wait "$deribit_private_pid"
unset deribit_private_pid
jq -s -e 'last.metrics.CODQueried == true and last.metrics.CODScope == "connection" and last.metrics.CODEnabled == true' "$e2e_tmp/deribit-private.jsonl" >/dev/null
rg -q "$deribit_fill_id" "$e2e_tmp/deribit-private.jsonl"
wait "$bybit_private_pid" || [[ $? == 124 ]]
unset bybit_private_pid

jq -n \
  --arg bybit_qty "$bybit_qty" \
  --arg bybit_entry_fee "$(jq -r '[.[].execFee|tonumber]|add' "$e2e_tmp/bybit-fill-trades.json")" \
  --arg bybit_exit_fee "$(jq -r '[.[].execFee|tonumber]|add' "$e2e_tmp/bybit-cleanup-trades.json")" \
  --arg deribit_amount "$deribit_amount" \
  --arg deribit_entry_fee "$read_fee" \
  --arg deribit_exit_fee "$(jq -r '[.[].fee|tonumber]|add' "$e2e_tmp/deribit-cleanup-trades.json")" \
  --arg bybit_fix_status "$bybit_fix_status" \
  --arg deribit_fix_status "$deribit_fix_status" \
  '{result:"PASS",independentReads:true,privateEvents:true,positionsZero:true,fix:{bybit:$bybit_fix_status,deribit:$deribit_fix_status},bybit:{amount:$bybit_qty,unit:"ETH",entryFee:$bybit_entry_fee,exitFee:$bybit_exit_fee},deribit:{amount:$deribit_amount,unit:"USD_notional",settlement:"BTC",entryFee:$deribit_entry_fee,exitFee:$deribit_exit_fee}}'
