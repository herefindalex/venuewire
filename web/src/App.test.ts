import { flushPromises, mount } from '@vue/test-utils';
import Antd from 'ant-design-vue';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

vi.mock('./api', async (importOriginal) => {
  const original = await importOriginal<typeof import('./api')>();
  return {
    ...original,
    setCSRF: vi.fn(),
    api: {
      login: vi.fn(),
      me: vi.fn(),
      logout: vi.fn(),
      venues: vi.fn(),
      account: vi.fn(),
      refreshAccount: vi.fn(),
      status: vi.fn(),
      trades: vi.fn(),
      quote: vi.fn(),
      confirm: vi.fn(),
      recheck: vi.fn(),
    },
  };
});

import App from './App.vue';
import { api, type AccountView, type QuoteView, type TradeView, type VenueStatus } from './api';

const mockedAPI = vi.mocked(api);

class FixtureWebSocket {
  static instances: FixtureWebSocket[] = [];
  onmessage: ((event: MessageEvent) => void) | null = null;
  onclose: (() => void) | null = null;
  sent: string[] = [];

  constructor(readonly url: string) {
    FixtureWebSocket.instances.push(this);
  }

  send(value: string) {
    this.sent.push(value);
  }

  close() {}
}

const account: AccountView = {
  venue: 'bybit',
  environment: 'testnet',
  accountAlias: 'bybit-demo',
  accountType: 'UNIFIED',
  revision: 7,
  snapshotAsOf: '2026-09-10T20:00:00Z',
  exchangeReportedTotalUsd: '55900',
  exchangeReportedAsOf: '2026-09-10T20:00:00Z',
  pricedSubtotalUsd: '56000',
  valuationBasis: 'public USD mark prices',
  completeness: 'partial',
  unpricedAssets: ['USDT'],
  liabilityStatus: 'none',
  hasDerivativePositions: null,
  derivativePositionEvidence: 'wallet snapshot does not prove derivative-position absence',
  assets: [
    {
      asset: 'BTC',
      balance: '0.5',
      availableToTrade: '0.5',
      availableStatus: 'verified',
      valuationQuantity: '0.5',
      quantityBasis: 'wallet balance',
      exchangeReportedUsdValue: '49000',
      usdValue: '50000',
      priceSource: 'Deribit btc_usd index',
      quality: 'fresh',
    },
  ],
};

const staleStatus: VenueStatus = {
  venue: 'bybit',
  rest: 'LIVE',
  publicWs: 'STALE',
  privateWs: 'LIVE',
  accountSync: 'STALE',
  reconnects: 3,
  publicReconnects: 2,
  privateReconnects: 1,
  requestErrors: 1,
  orderRequestErrors: 1,
  rateLimitState: 'LIMITED',
  reconciliationStatus: 'SYNCED',
  reconciliationDiscrepancies: 1,
  publicReceiveAgeMs: 500,
  publicEventAgeMs: 8400,
  privateReceiveAgeMs: 250,
};

const unknownTrade: TradeView = {
  intentId: 'trade_unknown_fixture',
  venue: 'bybit',
  routeId: 'bybit-usdt-btc',
  fromAsset: 'USDT',
  toAsset: 'BTC',
  requestedAmount: '100',
  status: 'Unknown',
  resultStatus: 'OUTCOME_UNKNOWN',
  clientOrderId: 'vw-unknown-fixture',
  fillDetailsStatus: 'PENDING',
  feeDetailsStatus: 'PENDING',
  balanceSyncStatus: 'PENDING',
  createdAt: '2026-09-10T20:00:00Z',
  updatedAt: '2026-09-10T20:00:02Z',
  lifecycle: [],
};

describe('VenueWire console', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    document.body.innerHTML = '';
    localStorage.clear();
    FixtureWebSocket.instances = [];
    vi.stubGlobal('WebSocket', FixtureWebSocket);
    vi.stubGlobal('matchMedia', vi.fn().mockImplementation((query: string) => ({
      matches: false,
      media: query,
      onchange: null,
      addListener: vi.fn(),
      removeListener: vi.fn(),
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
      dispatchEvent: vi.fn(),
    })));
    vi.stubGlobal('ResizeObserver', class {
      observe() {}
      unobserve() {}
      disconnect() {}
    });
    const getComputedStyle = window.getComputedStyle.bind(window);
    vi.spyOn(window, 'getComputedStyle').mockImplementation((element) => getComputedStyle(element));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('shows explicit VenueWire Testnet prototype branding before login', async () => {
    mockedAPI.me.mockRejectedValueOnce(new Error('not authenticated'));
    const wrapper = mount(App, { global: { plugins: [Antd] } });
    await flushPromises();

    expect(wrapper.text()).toContain('VenueWire');
    expect(wrapper.text()).toContain('Bybit · Deribit · TESTNET');
    expect(wrapper.text()).toContain('Testnet execution prototype. No real-money trading.');
    expect(FixtureWebSocket.instances).toHaveLength(0);
    wrapper.unmount();
  });

  it('renders stale status, separate valuations and an Unknown persisted trade', async () => {
    mockedAPI.me.mockResolvedValueOnce({
      username: 'demo',
      csrfToken: 'csrf-fixture',
      expiresAt: '2026-09-10T21:00:00Z',
      readOnly: false,
    });
    mockedAPI.venues.mockResolvedValueOnce({
      venues: [{ id: 'bybit', environment: 'testnet', accountAlias: 'bybit-demo' }],
      defaultVenue: 'bybit',
      tradingEnabled: true,
    });
    mockedAPI.account.mockResolvedValue({ account });
    mockedAPI.status.mockResolvedValue({
      venues: [staleStatus],
      build: { environment: 'Testnet', backend: 'Go fixture', frontend: 'Vue 3' },
    });
    mockedAPI.trades.mockResolvedValue({ trades: [unknownTrade] });

    const wrapper = mount(App, { attachTo: document.body, global: { plugins: [Antd] } });
    await flushPromises();
    await flushPromises();

    const text = wrapper.text();
    expect(text).toContain('VenueWire');
    expect(text).toContain('TESTNET');
    expect(text).toContain('bybit-demo');
    expect(text).toContain('Public STALE');
    expect(text).toContain('Private LIVE');
    expect(text).toContain('Local USD mark');
    expect(text).toContain('$56000');
    expect(text).toContain('priced subtotal');
    expect(text).toContain('Exchange-reported total');
    expect(text).toContain('$55900');
    expect(text).toContain('UNIFIED · liabilities none');
    expect(text).not.toContain('derivatives unknown');
    expect(text).toContain('Account scope caution');
    expect(text).toContain('STALE');
    expect(text).toContain('LIMITED');
    expect(text).toContain('Unknown');
    expect(text).toContain('2.0 s');
    expect(text).toContain('Equity');
    expect(FixtureWebSocket.instances[0]?.url).toContain('/api/ws');

    (wrapper.vm as unknown as { showTrade: (trade: TradeView) => void }).showTrade(unknownTrade);
    await flushPromises();
    await vi.waitFor(() => {
      const recheckButton = document.body.querySelector('.recheck-button');
      expect(recheckButton).not.toBeNull();
      expect(recheckButton?.classList.contains('ant-btn-primary')).toBe(true);
      expect(recheckButton?.classList.contains('ant-btn-lg')).toBe(true);
    });
    wrapper.unmount();
  });

  it('labels a missing venue aggregate instead of hiding the exchange-reported total', async () => {
    mockedAPI.me.mockResolvedValueOnce({
      username: 'demo',
      csrfToken: 'csrf-fixture',
      expiresAt: '2026-09-10T21:00:00Z',
      readOnly: false,
    });
    mockedAPI.venues.mockResolvedValueOnce({
      venues: [{ id: 'deribit', environment: 'testnet', accountAlias: 'deribit-demo' }],
      defaultVenue: 'deribit',
      tradingEnabled: true,
    });
    mockedAPI.account.mockResolvedValue({
      account: {
        ...account,
        venue: 'deribit',
        accountAlias: 'deribit-demo',
        accountType: 'account summaries',
        exchangeReportedTotalUsd: undefined,
        exchangeReportedAsOf: undefined,
      },
    });
    mockedAPI.status.mockResolvedValue({ venues: [staleStatus], build: {} });
    mockedAPI.trades.mockResolvedValue({ trades: [] });

    const wrapper = mount(App, { global: { plugins: [Antd] } });
    await flushPromises();
    await flushPromises();

    expect(wrapper.text()).toContain('Exchange-reported total');
    expect(wrapper.text()).toContain('Not reported by venue');
    expect(wrapper.text()).toContain('No aggregate USD total in the venue response');
    wrapper.unmount();
  });

  it('shows the shared account store and balance sync in the trade result modal', async () => {
    mockedAPI.me.mockResolvedValueOnce({
      username: 'demo', csrfToken: 'csrf-fixture', expiresAt: '2026-09-10T21:00:00Z', readOnly: false,
    });
    mockedAPI.venues.mockResolvedValueOnce({
      venues: [{ id: 'bybit', environment: 'testnet', accountAlias: 'bybit-demo' }], defaultVenue: 'bybit', tradingEnabled: true,
    });
    mockedAPI.account.mockResolvedValue({ account });
    mockedAPI.status.mockResolvedValue({ venues: [staleStatus], build: {} });
    mockedAPI.trades.mockResolvedValue({ trades: [] });
    const quote: QuoteView = {
      quoteId: 'quote-fixture', venue: 'bybit', routeId: 'bybit-usdt-btc', fromAsset: 'USDT', toAsset: 'BTC',
      spendBudget: '100', instrument: 'BTCUSDT', side: 'Buy', baseQty: '0.001', limitPrice: '100500', timeInForce: 'IOC',
      referenceBid: '99900', referenceAsk: '100000', bookObservedAt: new Date().toISOString(), priceProtectionBps: 50,
      grossReceiveEstimate: '0.001', netReceiveEstimate: '0.000999', sourceDebitUpperBound: '100',
      createdAt: new Date().toISOString(), expiresAt: new Date(Date.now() + 5000).toISOString(), warnings: [], executable: true,
    };
    mockedAPI.quote.mockResolvedValue({ quote });
    mockedAPI.confirm.mockResolvedValue({
      trade: { ...unknownTrade, status: 'Filled', resultStatus: 'FILLED', balanceSyncStatus: 'SYNCED' },
    });

    const wrapper = mount(App, { attachTo: document.body, global: { plugins: [Antd] } });
    await flushPromises();
    const quickTrade = wrapper.findAll('button').find((button) => button.text().includes('Quick Trade'));
    expect(quickTrade).toBeDefined();
    await quickTrade!.trigger('click');
    await flushPromises();
    const amount = document.body.querySelector<HTMLInputElement>('input[placeholder="0.00"]');
    expect(amount).not.toBeNull();
    amount!.value = '100';
    amount!.dispatchEvent(new Event('input', { bubbles: true }));
    const reviewForm = wrapper.findComponent({ name: 'AForm' });
    expect(reviewForm.exists()).toBe(true);
    reviewForm.vm.$emit('finish');
    await flushPromises();
    expect(mockedAPI.quote).toHaveBeenCalledTimes(1);
    let confirm: HTMLButtonElement | undefined;
    await vi.waitFor(() => {
      confirm = [...document.body.querySelectorAll('button')].find((button) => button.textContent?.includes('Confirm Testnet trade'));
      expect(confirm).toBeDefined();
    });
    confirm!.click();
    await flushPromises();

    const text = document.body.textContent || '';
    expect(text).toContain('Account snapshot');
    expect(text).toContain('Balance SYNCED');
    expect(text).toContain('Same bybit-demo store as the page');
    expect(text).toContain('0.5');
    expect(mockedAPI.confirm).toHaveBeenCalledTimes(1);
    wrapper.unmount();
  });
});
