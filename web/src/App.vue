<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, reactive, ref, watch } from 'vue';
import { message } from 'ant-design-vue';
import { api, APIError, setCSRF, type AccountView, type QuoteView, type SessionView, type TradeView, type VenueStatus, type VenueView } from './api';

const session = ref<SessionView>();
const checkingSession = ref(true);
const credentials = reactive({ username: '', password: '' });
const signingIn = ref(false);
const venues = ref<VenueView[]>([]);
const selectedVenue = ref<'bybit' | 'deribit'>('bybit');
const account = ref<AccountView>();
const statuses = ref<VenueStatus[]>([]);
const trades = ref<TradeView[]>([]);
const loadingData = ref(false);
const aboutOpen = ref(false);
const tradeDrawerOpen = ref(false);
const selectedTrade = ref<TradeView>();
const quickTradeOpen = ref(false);
const tradeStep = ref<'edit' | 'review' | 'submitting' | 'result'>('edit');
const spendAmount = ref('');
const direction = ref('');
const quote = ref<QuoteView>();
const quoteNow = ref(Date.now());
const quoteLoading = ref(false);
const rechecking = ref(false);
const refreshingAccount = ref(false);
const buildInfo = ref<Record<string, string>>({});
let quoteTimer: number | undefined;
let pollTimer: number | undefined;
let socket: WebSocket | undefined;

const routeOptions = computed(() => selectedVenue.value === 'bybit'
  ? [{ label: 'USDT → BTC', value: 'bybit-usdt-btc' }, { label: 'BTC → USDT', value: 'bybit-btc-usdt' }]
  : [{ label: 'BTC → ETH', value: 'deribit-btc-eth' }, { label: 'ETH → BTC', value: 'deribit-eth-btc' }]);
const quoteRemaining = computed(() => quote.value ? Math.max(0, Math.ceil((Date.parse(quote.value.expiresAt) - quoteNow.value) / 1000)) : 0);
const canConfirm = computed(() => !!quote.value?.executable && quoteRemaining.value > 0 && !session.value?.readOnly);
const selectedStatus = computed(() => statuses.value.find((item) => item.venue === selectedVenue.value));

function readableError(error: unknown) {
  if (error instanceof APIError) return error.requestId ? `${error.message} (${error.requestId})` : error.message;
  return 'The request could not be completed.';
}

async function signIn() {
  signingIn.value = true;
  try {
    const result = await api.login(credentials.username, credentials.password);
    setCSRF(result.csrfToken);
    session.value = result;
    credentials.password = '';
    await bootstrap();
  } catch (error) {
    message.error(readableError(error));
  } finally {
    signingIn.value = false;
  }
}

async function logout() {
  try { await api.logout(); } catch { /* local state still clears */ }
  socket?.close();
  session.value = undefined;
  venues.value = [];
  account.value = undefined;
}

async function bootstrap() {
  loadingData.value = true;
  try {
    const venueResult = await api.venues();
    venues.value = venueResult.venues;
    const saved = localStorage.getItem('venuewire.venue');
    const initial = venueResult.venues.some((item) => item.id === saved) ? saved! : venueResult.defaultVenue;
    selectedVenue.value = initial as 'bybit' | 'deribit';
    await refreshAll();
    connectSocket();
  } catch (error) {
    message.error(readableError(error));
  } finally {
    loadingData.value = false;
  }
}

async function refreshAll() {
  const results = await Promise.allSettled([api.account(selectedVenue.value), api.status(), api.trades()]);
  if (results[0].status === 'fulfilled') account.value = results[0].value.account;
  if (results[1].status === 'fulfilled') {
    statuses.value = results[1].value.venues;
    buildInfo.value = results[1].value.build;
  }
  if (results[2].status === 'fulfilled') trades.value = results[2].value.trades;
}

async function refreshAccount() {
  refreshingAccount.value = true;
  try {
    account.value = (await api.refreshAccount(selectedVenue.value)).account;
    const runtime = await api.status();
    statuses.value = runtime.venues;
    buildInfo.value = runtime.build;
  } catch (error) {
    message.error(readableError(error));
  } finally {
    refreshingAccount.value = false;
  }
}

function connectSocket() {
  socket?.close();
  socket = new WebSocket(`${location.protocol === 'https:' ? 'wss' : 'ws'}://${location.host}/api/ws`);
  socket.onmessage = () => void refreshAll();
  socket.onclose = () => {
    if (session.value) window.setTimeout(connectSocket, 2000);
  };
}

watch(selectedVenue, async (venue) => {
  localStorage.setItem('venuewire.venue', venue);
  direction.value = routeOptions.value[0]?.value ?? '';
  account.value = undefined;
  if (session.value) {
    const response = await api.account(venue).catch(() => undefined);
    account.value = response?.account;
  }
});

function openQuickTrade() {
  direction.value = routeOptions.value[0]?.value ?? '';
  spendAmount.value = '';
  quote.value = undefined;
  tradeStep.value = 'edit';
  quickTradeOpen.value = true;
}

async function reviewTrade() {
  quoteLoading.value = true;
  try {
    quote.value = (await api.quote(selectedVenue.value, direction.value, spendAmount.value)).quote;
    tradeStep.value = 'review';
    quoteNow.value = Date.now();
    quoteTimer = window.setInterval(() => { quoteNow.value = Date.now(); }, 250);
  } catch (error) {
    message.error(readableError(error));
  } finally { quoteLoading.value = false; }
}

async function confirmTrade() {
  if (!quote.value || !canConfirm.value) return;
  tradeStep.value = 'submitting';
  try {
    selectedTrade.value = (await api.confirm(quote.value.quoteId, crypto.randomUUID())).trade;
    tradeStep.value = 'result';
    await refreshAll();
  } catch (error) {
    message.error(readableError(error));
    tradeStep.value = 'review';
  }
}

function showTrade(trade: TradeView) {
  selectedTrade.value = trade;
  tradeDrawerOpen.value = true;
}

async function recheckTrade() {
  if (!selectedTrade.value) return;
  rechecking.value = true;
  try {
    selectedTrade.value = (await api.recheck(selectedTrade.value.intentId)).trade;
    await refreshAll();
  } catch (error) { message.error(readableError(error)); }
  finally { rechecking.value = false; }
}

onMounted(async () => {
  try {
    const result = await api.me();
    setCSRF(result.csrfToken);
    session.value = result;
    await bootstrap();
  } catch { session.value = undefined; }
  finally { checkingSession.value = false; }
  pollTimer = window.setInterval(() => { if (session.value) void refreshAll(); }, 15000);
});

onBeforeUnmount(() => {
  if (quoteTimer) clearInterval(quoteTimer);
  if (pollTimer) clearInterval(pollTimer);
  socket?.close();
});
</script>

<template>
  <a-spin v-if="checkingSession" class="startup-spin" size="large" />
  <main v-else-if="!session" class="login-shell">
    <section class="login-card">
      <div class="brand-mark">VW</div>
      <p class="eyebrow">TESTNET CONNECTIVITY</p>
      <h1>VenueWire</h1>
      <p class="login-subtitle">Multi-Venue Trading Connectivity Console</p>
      <a-tag color="gold">Bybit · Deribit · TESTNET</a-tag>
      <a-form layout="vertical" class="login-form" @finish="signIn">
        <a-form-item label="Username"><a-input v-model:value="credentials.username" autocomplete="username" /></a-form-item>
        <a-form-item label="Password"><a-input-password v-model:value="credentials.password" autocomplete="current-password" /></a-form-item>
        <a-button type="primary" html-type="submit" block size="large" :loading="signingIn">Sign in</a-button>
      </a-form>
      <p class="prototype-note">Testnet execution prototype. No real-money trading.</p>
    </section>
  </main>

  <a-layout v-else class="console-shell">
    <a-layout-header class="topbar">
      <div><span class="wordmark">VenueWire</span><span class="wordmark-detail">Connectivity Console</span></div>
      <div class="topbar-actions">
        <a-select v-model:value="selectedVenue" class="venue-select" :options="venues.map(v => ({ label: v.id === 'bybit' ? 'Bybit' : 'Deribit', value: v.id }))" />
        <a-tag color="gold" class="testnet-badge">TESTNET</a-tag>
        <a-button type="text" @click="aboutOpen = true">About</a-button>
        <a-button type="text" @click="logout">Logout</a-button>
      </div>
    </a-layout-header>

    <a-layout-content class="content" :aria-busy="loadingData">
      <div class="hero-row">
        <div>
          <p class="eyebrow">{{ selectedVenue.toUpperCase() }} · TESTNET ACCOUNT</p>
          <h2>Account overview</h2>
          <p class="muted">Snapshot {{ account?.snapshotAsOf ? new Date(account.snapshotAsOf).toLocaleTimeString() : 'unavailable' }}</p>
        </div>
        <div class="hero-actions">
          <div class="account-value"><span>Locally marked value</span><strong>{{ account?.totalUsd ? `$${account.totalUsd}` : '—' }}</strong><small>{{ account?.completeness ?? 'Unavailable' }}</small></div>
          <a-button type="primary" size="large" :disabled="!account" @click="openQuickTrade">Quick Trade</a-button>
        </div>
      </div>

      <a-alert v-if="session.readOnly" message="View-only demo" description="Trading is disabled by the server. Quotes remain available for review." type="info" show-icon class="section-gap" />

      <section class="panel section-gap">
        <div class="section-heading"><div><p class="eyebrow">BALANCES</p><h3>Assets</h3></div><a-button :loading="refreshingAccount" @click="refreshAccount">Refresh from venue</a-button></div>
        <a-table :data-source="account?.assets ?? []" :pagination="false" row-key="asset" size="middle">
          <a-table-column title="Asset" data-index="asset"><template #default="{ text }"><strong>{{ text }}</strong></template></a-table-column>
          <a-table-column title="Balance" data-index="balance" />
          <a-table-column title="Available to trade" data-index="availableToTrade"><template #default="{ text }">{{ text || '—' }}</template></a-table-column>
          <a-table-column title="USD value" data-index="usdValue"><template #default="{ text }">{{ text ? `$${text}` : '—' }}</template></a-table-column>
          <a-table-column title="Quality" data-index="quality"><template #default="{ text }"><a-tag :color="text === 'live' ? 'green' : text === 'stale' ? 'orange' : 'default'">{{ text }}</a-tag></template></a-table-column>
        </a-table>
      </section>

      <div class="two-column section-gap">
        <section class="panel">
          <div class="section-heading"><div><p class="eyebrow">RUNTIME</p><h3>System status</h3></div><span class="muted">Backend measurements</span></div>
          <div v-if="statuses.length" class="status-list">
            <div v-for="status in statuses" :key="status.venue" class="status-card">
              <div class="status-title"><strong>{{ status.venue }}</strong><a-tag :color="status.accountSync === 'SYNCED' ? 'green' : 'orange'">{{ status.accountSync }}</a-tag></div>
              <dl><dt>REST</dt><dd>{{ status.rest }}</dd><dt>Public WS</dt><dd>{{ status.publicWs }}</dd><dt>Private WS</dt><dd>{{ status.privateWs }}</dd><dt>Market age</dt><dd>{{ status.marketAgeMs == null ? '—' : `${status.marketAgeMs} ms` }}</dd><dt>Reconnects</dt><dd>{{ status.reconnects }}</dd></dl>
            </div>
          </div>
          <a-empty v-else description="Runtime status unavailable" />
        </section>

        <section class="panel">
          <div class="section-heading"><div><p class="eyebrow">ORDER LIFECYCLE</p><h3>Recent trades</h3></div><span class="muted">Shared demo history</span></div>
          <a-table :data-source="trades" :pagination="false" row-key="intentId" size="small" :custom-row="(record: TradeView) => ({ onClick: () => showTrade(record) })">
            <a-table-column title="Time" data-index="createdAt"><template #default="{ text }">{{ new Date(text).toLocaleTimeString() }}</template></a-table-column>
            <a-table-column title="Venue" data-index="venue" />
            <a-table-column title="Direction"><template #default="{ record }">{{ record.fromAsset }} → {{ record.toAsset }}</template></a-table-column>
            <a-table-column title="Status" data-index="status"><template #default="{ text }"><a-tag :color="text === 'Filled' ? 'green' : text === 'Unknown' ? 'orange' : 'blue'">{{ text }}</a-tag></template></a-table-column>
          </a-table>
        </section>
      </div>
    </a-layout-content>
  </a-layout>

  <a-modal v-model:open="quickTradeOpen" width="620px" :footer="null" :mask-closable="tradeStep === 'edit' || tradeStep === 'review'" title="Quick Trade">
    <a-tag color="gold" class="modal-testnet">TESTNET</a-tag>
    <div v-if="tradeStep === 'edit'" class="modal-body">
      <a-form layout="vertical" @finish="reviewTrade">
        <a-form-item label="Direction"><a-select v-model:value="direction" :options="routeOptions" /></a-form-item>
        <a-form-item label="Source amount"><a-input v-model:value="spendAmount" inputmode="decimal" placeholder="0.00" /></a-form-item>
        <a-button type="primary" html-type="submit" block :loading="quoteLoading">Review protected IOC</a-button>
      </a-form>
    </div>
    <div v-else-if="tradeStep === 'review' && quote" class="modal-body">
      <div class="review-heading"><span>{{ quote.fromAsset }} → {{ quote.toAsset }}</span><strong>{{ quote.spendBudget }} {{ quote.fromAsset }}</strong></div>
      <a-descriptions bordered :column="1" size="small">
        <a-descriptions-item label="Reference bid / ask">{{ quote.referenceBid || '—' }} / {{ quote.referenceAsk || '—' }}</a-descriptions-item>
        <a-descriptions-item label="Submitted base quantity">{{ quote.baseQty }}</a-descriptions-item>
        <a-descriptions-item label="Worst acceptable price">{{ quote.limitPrice }}</a-descriptions-item>
        <a-descriptions-item label="Protection">{{ quote.priceProtectionBps / 100 }}%</a-descriptions-item>
        <a-descriptions-item label="Estimated net received">{{ quote.netReceiveEstimate || '—' }} {{ quote.toAsset }}</a-descriptions-item>
        <a-descriptions-item label="Quote expires"><a-tag :color="quoteRemaining > 1 ? 'green' : 'red'">{{ quoteRemaining }}s</a-tag></a-descriptions-item>
      </a-descriptions>
      <a-alert v-for="warning in quote.warnings" :key="warning" :message="warning" type="warning" show-icon class="review-warning" />
      <div class="modal-actions"><a-button @click="tradeStep = 'edit'">Back</a-button><a-button type="primary" :disabled="!canConfirm" @click="confirmTrade">Confirm Testnet trade</a-button></div>
    </div>
    <div v-else-if="tradeStep === 'submitting'" class="submitting-state"><a-spin size="large" /><h3>Submitting saved intent</h3><p>The result remains tracked if this window closes.</p></div>
    <div v-else-if="selectedTrade" class="modal-body"><a-result :status="selectedTrade.status === 'Filled' ? 'success' : selectedTrade.status === 'Unknown' ? 'warning' : 'info'" :title="selectedTrade.resultStatus || selectedTrade.status" :sub-title="selectedTrade.message || `VenueWire Trade ID: ${selectedTrade.intentId}`"><template #extra><a-button @click="quickTradeOpen = false">Close</a-button><a-button type="primary" @click="showTrade(selectedTrade); quickTradeOpen = false">View lifecycle</a-button></template></a-result></div>
  </a-modal>

  <a-drawer v-model:open="tradeDrawerOpen" title="Trade lifecycle" width="520px">
    <template v-if="selectedTrade">
      <div class="drawer-title"><div><p class="eyebrow">{{ selectedTrade.venue }} · TESTNET</p><h3>{{ selectedTrade.fromAsset }} → {{ selectedTrade.toAsset }}</h3></div><a-tag>{{ selectedTrade.status }}</a-tag></div>
      <a-descriptions :column="1" size="small" bordered><a-descriptions-item label="VenueWire Trade ID">{{ selectedTrade.intentId }}</a-descriptions-item><a-descriptions-item label="Client Order ID">{{ selectedTrade.clientOrderId }}</a-descriptions-item><a-descriptions-item label="Venue Order ID">{{ selectedTrade.venueOrderId || 'Pending' }}</a-descriptions-item><a-descriptions-item label="Filled quantity">{{ selectedTrade.filledBaseQty || 'Pending' }}</a-descriptions-item><a-descriptions-item label="Average price">{{ selectedTrade.averagePrice || 'Pending' }}</a-descriptions-item><a-descriptions-item label="Net received">{{ selectedTrade.netDestinationReceived || 'Pending' }}</a-descriptions-item></a-descriptions>
      <a-button block class="recheck-button" :loading="rechecking" @click="recheckTrade">Recheck with venue</a-button>
      <a-timeline class="trade-timeline"><a-timeline-item v-for="event in selectedTrade.lifecycle" :key="`${event.at}-${event.status}`"><strong>{{ event.status }}</strong><br><span class="muted">{{ new Date(event.at).toLocaleString() }}</span><p v-if="event.detail">{{ event.detail }}</p></a-timeline-item></a-timeline>
    </template>
  </a-drawer>

  <a-drawer v-model:open="aboutOpen" title="About VenueWire" width="560px">
    <p class="eyebrow">MULTI-VENUE TRADING CONNECTIVITY CONSOLE</p><h2>VenueWire</h2><p>Testnet execution prototype integrating Bybit and Deribit through normalized account, quote, order lifecycle, recovery and observability boundaries.</p>
    <pre class="architecture">Browser
  │ HTTPS / WebSocket
Nginx
  │ private network
VenueWire Go Backend
  ├─ Venue adapters
  ├─ Normalized domain model
  ├─ Order state machine
  ├─ Reconciliation
  └─ Valuation & observability
       ├─ Bybit Testnet
       └─ Deribit Testnet</pre>
    <a-descriptions :column="1" bordered size="small"><a-descriptions-item label="Environment">{{ buildInfo.environment ?? 'Testnet' }}</a-descriptions-item><a-descriptions-item label="Backend">{{ buildInfo.backend ?? 'Go' }}</a-descriptions-item><a-descriptions-item label="Frontend">{{ buildInfo.frontend ?? 'Vue 3' }}</a-descriptions-item><a-descriptions-item label="Build timestamp">{{ buildInfo.buildTimestamp ?? 'development' }}</a-descriptions-item><a-descriptions-item label="Git commit">{{ buildInfo.gitCommit ?? 'development' }}</a-descriptions-item><a-descriptions-item label="Uptime">{{ buildInfo.uptime ?? '—' }}</a-descriptions-item><a-descriptions-item label="Capabilities">REST / JSON-RPC · WebSocket · FIX 4.4 where verified</a-descriptions-item></a-descriptions>
  </a-drawer>
</template>
